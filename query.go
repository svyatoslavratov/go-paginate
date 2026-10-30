package paginate

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// Query is a request checked against an Endpoint and rendered for SQL: what an executor adds to
// its base query. gormx and squirrelx run it; another executor (database/sql, sqlx, bun) runs it
// the same way:
//
//  1. AND every Where condition to the base query;
//  2. count the matching rows as Count says: COUNT(*) for CountExact, EXPLAIN for CountEstimate
//     (EstimateFromPlan reads it, and an estimate below ExactCountBelow is counted exactly),
//     nothing for CountNone;
//  3. unless Mode is ModeCount, read the rows ordered by OrderBy, with LIMIT FetchLimit() and
//     OFFSET Offset when Mode is ModeDefault;
//  4. build the answer with NewResponse.
type Query struct {
	// Where holds the search, the filters and the keyset condition of cursor pagination, each safe
	// to AND with the base query and with the others.
	Where []Condition
	// Sort is the effective sort, without the tie-breaker.
	Sort []SortEntry
	// TieBreak is the tie-breaker column, empty for none. OrderBy appends it.
	TieBreak string
	// Limit and Offset select the page; ignore them unless Mode is ModeDefault. Offset is zero in
	// cursor pagination. Read Limit through FetchLimit, which adds a row to look ahead.
	Limit  uint64
	Offset uint64
	// Mode is the mode the request runs in.
	Mode Mode
	// Count is how to count the matching rows of this request; CountNone means not at all.
	Count Count
	// ExactCountBelow replaces an estimate below it with an exact count.
	ExactCountBelow uint64
	// Parallel asks to run the count and the page at the same time, where the connection allows.
	Parallel bool
	// Keyset is set in cursor pagination: Where holds the keyset condition, which relies on ORDER BY
	// ending with the tie-breaker.
	Keyset bool
	// WithDeleted is set when the client asked for soft-deleted rows and the config allows it.
	WithDeleted bool

	// req is the normalized request, for the answer.
	req  Request
	view *responseView
	// nullSort is the NULL placement of the endpoint.
	nullSort NullSort
	// clientValues marks client values among the arguments, for WrapError.
	clientValues bool
	// cursor is the keyset of cursor pagination, nil otherwise.
	cursor *keyset
	// backward is set when the request pages back from its cursor.
	backward bool
}

// OrderBy renders the ORDER BY expressions, with the tie-breaker last when withTieBreak is set. An
// executor leaves it out where the database would reject it (DISTINCT, GROUP BY without it); cursor
// pagination cannot do without it. Paging back from a cursor, the order is reversed, and
// NewResponse turns the rows back.
func (q *Query) OrderBy(withTieBreak bool) []string {
	entries := q.Sort
	if withTieBreak {
		entries = appendTieBreak(slices.Clone(q.Sort), q.TieBreak)
	}
	return orderByExprs(entries, q.nullSort, q.backward)
}

// FetchLimit is the LIMIT of the page: Limit, plus one row to look ahead when the answer must tell
// a next page without a count (CountNone, cursor pagination). NewResponse drops the extra row.
func (q *Query) FetchLimit() uint64 {
	if q.lookahead() {
		return q.Limit + 1
	}
	return q.Limit
}

func (q *Query) lookahead() bool {
	return q.Mode == ModeDefault && (q.cursor != nil || q.Count == CountNone)
}

// WrapError wraps a failed statement of stage ("count", "select"). A Postgres data exception caused
// by a client value of the wrong type ("abc" for an integer) becomes ErrInvalidRequest.
func (q *Query) WrapError(stage string, err error) error {
	return databaseError(stage, err, q.clientValues)
}

// Prepare checks req against the endpoint and renders it. It normalizes req in place (page, limit,
// mode) and keeps a copy for NewResponse. Errors wrap ErrInvalidRequest, answered with 400, and
// carry a Code.
func (e *Endpoint) Prepare(req *Request) (*Query, error) {
	cfg := &e.cfg
	if err := e.normalizeRequest(req); err != nil {
		return nil, err
	}
	q := &Query{
		TieBreak:        cfg.TieBreak,
		Mode:            req.Mode,
		Limit:           req.Limit,
		Offset:          (req.Page - 1) * req.Limit,
		Count:           e.count(req.Mode),
		ExactCountBelow: cfg.ExactCountBelow,
		Parallel:        cfg.ParallelCount,
		WithDeleted:     req.WithDeleted && cfg.AllowWithDeletedInQuery,
		nullSort:        cfg.NullSort,
		clientValues:    req.hasClientValues(),
	}

	searchFields, err := e.searchFields(req)
	if err != nil {
		return nil, err
	}
	if terms := searchTerms(req.Search, cfg.MultiWordSearch); len(searchFields) != 0 && len(terms) != 0 {
		columns := make([]string, len(searchFields))
		for i, field := range searchFields {
			columns[i] = field.Column
		}
		q.Where = append(q.Where, condition(searchExpr(columns, terms)))
	}
	filters, err := e.filters(req)
	if err != nil {
		return nil, err
	}
	if len(filters) != 0 {
		expr, err := filterExpr(filters, cfg.CompareAsText)
		if err != nil {
			return nil, err
		}
		q.Where = append(q.Where, condition(expr))
	}
	if req.FilterExpression != "" {
		node, err := parseFilterExpression(req.FilterExpression, cfg.MaxFilterExpressionComplexity)
		if err != nil {
			return nil, err
		}
		expr, err := node.render(e)
		if err != nil {
			return nil, err
		}
		q.Where = append(q.Where, condition(expr))
	}

	keys, err := e.sortKeys(req)
	if err != nil {
		return nil, err
	}
	q.Sort = make([]SortEntry, len(keys))
	for i, key := range keys {
		q.Sort[i] = SortEntry{Column: key.field.Column, Order: key.order}
	}
	if cfg.Pagination == CursorPagination && req.Mode == ModeDefault {
		if err := e.applyCursor(q, req.Cursor, keys); err != nil {
			return nil, err
		}
	}

	q.req = *req
	q.view = e.describe(req, keys, searchFields)
	return q, nil
}

func condition(expr sqlExpr) Condition {
	return Condition{SQL: expr.sql, Args: expr.args}
}

// normalizeRequest fixes the page, the limit and the mode of a request, and checks what does not
// depend on fields.
func (e *Endpoint) normalizeRequest(req *Request) error {
	cfg := &e.cfg
	if req.Page == 0 || cfg.Pagination == CursorPagination {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = cfg.DefaultLimit
	}
	req.Limit = min(req.Limit, cfg.MaxLimit)
	// The offset (page-1)*limit must fit a signed integer: GORM takes an int, Postgres a bigint.
	if req.Page-1 > uint64(math.MaxInt)/req.Limit {
		return invalidRequest(CodeInvalidPage, "", "page %d is too large", req.Page)
	}
	// A client's "disabled" waits in RequestedMode with Mode unset; a mode the server set in the
	// meantime wins.
	if req.Mode == "" {
		switch {
		case req.RequestedMode == ModeCount:
			req.Mode = ModeCount
		case req.RequestedMode == ModeDisabled && cfg.AllowDisabledPagination:
			req.Mode = ModeDisabled
		default:
			req.Mode = ModeDefault
		}
	}
	if uint64(utf8.RuneCountInString(req.Search)) > cfg.MaxSearchLength {
		return invalidRequest(CodeSearchTooLong, "", "search is longer than %d characters", cfg.MaxSearchLength)
	}
	// The parsers check the orders already; requests built in code are checked here, since the
	// order goes into ORDER BY as text. The keys are copied: the caller's slice may be shared.
	if len(req.SortBy) != 0 {
		sortBy := make([]Sort, len(req.SortBy))
		for i, key := range req.SortBy {
			order, ok := parseOrder(string(key.Order))
			if !ok {
				return invalidRequest(CodeInvalidSort, key.Field, "sort order of %s must be %s or %s", key.Field, Asc, Desc)
			}
			sortBy[i] = Sort{Field: key.Field, Order: order}
		}
		req.SortBy = sortBy
	}
	return nil
}

// count is how a request in mode is counted. Every row is counted by reading it; a count-only
// request is counted even under CountNone.
func (e *Endpoint) count(mode Mode) Count {
	switch {
	case mode == ModeDisabled:
		return CountNone
	case mode == ModeCount && e.cfg.Count == CountNone:
		return CountExact
	}
	return e.cfg.Count
}

// searchFields returns the fields the search covers: the searchBy fields of the request, or every
// searchable field when the request has none or IgnoreSearchByInQueryParam is set.
func (e *Endpoint) searchFields(req *Request) ([]*Field, error) {
	if len(req.SearchBy) == 0 || e.cfg.IgnoreSearchByInQueryParam {
		return e.searchable, nil
	}
	fields := make([]*Field, 0, len(req.SearchBy))
	for _, name := range req.SearchBy {
		field, ok := e.fields[name]
		if !ok || !field.Searchable {
			if e.cfg.Lenient {
				continue
			}
			return nil, invalidRequest(CodeSearchNotAllowed, name, "searching by %s is not allowed", name)
		}
		if !slices.Contains(fields, field) {
			fields = append(fields, field)
		}
	}
	return fields, nil
}

// sortKeys returns the sort of a request: its keys in order, the first occurrence of a field
// winning, or the default sort.
func (e *Endpoint) sortKeys(req *Request) ([]sortKey, error) {
	keys := make([]sortKey, 0, len(req.SortBy))
	for _, key := range req.SortBy {
		field, ok := e.fields[key.Field]
		if !ok || !field.Sortable {
			if e.cfg.Lenient {
				continue
			}
			return nil, invalidRequest(CodeSortNotAllowed, key.Field, "sorting by %s is not allowed", key.Field)
		}
		if !slices.ContainsFunc(keys, func(k sortKey) bool { return k.field == field }) {
			keys = append(keys, sortKey{field: field, order: key.Order})
		}
	}
	if len(keys) == 0 {
		return slices.Clone(e.defaultSort), nil
	}
	return keys, nil
}

// responseView keeps what an answer needs from a prepared request and its endpoint.
type responseView struct {
	format     Format
	baseURL    string
	pagination Pagination
	// sort is the effective sort in API names, without the tie-breaker.
	sort []Sort
	// searchBy are the API fields the search covers.
	searchBy []string
	// searchByInLinks is set when the client narrowed the search itself.
	searchByInLinks bool
}

func (e *Endpoint) describe(req *Request, keys []sortKey, searchFields []*Field) *responseView {
	view := &responseView{
		format:          e.cfg.Format,
		baseURL:         strings.TrimSuffix(e.cfg.LinksBaseURL, "/"),
		pagination:      e.cfg.Pagination,
		sort:            make([]Sort, len(keys)),
		searchBy:        make([]string, len(searchFields)),
		searchByInLinks: len(req.SearchBy) != 0 && !e.cfg.IgnoreSearchByInQueryParam,
	}
	for i, key := range keys {
		view.sort[i] = Sort{Field: key.field.Name, Order: key.order}
	}
	for i, field := range searchFields {
		view.searchBy[i] = field.Name
	}
	return view
}
