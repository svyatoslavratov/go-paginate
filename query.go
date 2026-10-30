package paginate

import (
	"maps"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// Query is a request checked against a Config and rendered for SQL: what an executor adds to its
// base query. gormx and squirrelx run it; another executor (database/sql, sqlx, bun)
// can run it the same way.
type Query struct {
	// Where holds the search and the filters, each safe to AND with the base query and with each
	// other.
	Where []Condition
	// Sort is the effective sort, without the tie-breaker.
	Sort []SortEntry
	// TieBreak is the tie-breaker column, empty for none. OrderBy appends it.
	TieBreak string
	// Limit and Offset select the page; ignore them unless Mode is ModeDefault.
	Limit  uint64
	Offset uint64
	// Mode is the mode the request runs in.
	Mode Mode
	// WithDeleted is set when the client asked for soft-deleted rows and the config allows it.
	WithDeleted bool

	nullSort NullSort
	filtered bool
}

// OrderBy renders the ORDER BY expressions, with the tie-breaker last when withTieBreak is set.
// An executor leaves it out where the database would reject it (DISTINCT, GROUP BY without it).
func (q *Query) OrderBy(withTieBreak bool) []string {
	entries := q.Sort
	if withTieBreak {
		entries = appendTieBreak(slices.Clone(q.Sort), q.TieBreak)
	}
	return orderByExprs(entries, q.nullSort)
}

// WrapError wraps a failed statement of stage ("count", "select"). A Postgres data exception
// caused by a filter value of the wrong type ("abc" for an integer) becomes ErrInvalidRequest.
func (q *Query) WrapError(stage string, err error) error {
	return databaseError(stage, err, q.filtered)
}

// Prepare checks req against cfg and renders it. It normalizes req in place (page, limit, mode)
// and records what NewResponse needs for the meta and the links. Errors wrap ErrInvalidRequest
// (answer 400) or ErrInvalidConfig.
func Prepare(req *Request, cfg Config) (*Query, error) {
	if err := normalizeConfig(&cfg); err != nil {
		return nil, err
	}
	columns, err := normalizeRequest(req, &cfg)
	if err != nil {
		return nil, err
	}

	q := &Query{
		TieBreak:    cfg.TieBreakColumn,
		Mode:        req.Mode,
		Limit:       req.Limit,
		Offset:      (req.Page - 1) * req.Limit,
		WithDeleted: req.WithDeleted && cfg.AllowWithDeletedInQuery,
		nullSort:    cfg.NullSort,
		filtered:    req.hasFilters(),
	}

	searchBy := searchColumns(columns.searchBy, &cfg)
	if terms := searchTerms(req.Search, &cfg); len(searchBy) != 0 && len(terms) != 0 {
		q.Where = append(q.Where, condition(searchExpr(searchBy, terms)))
	}
	filters, err := parseFilters(columns.filter, &cfg)
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
		expr, err := node.render(&cfg)
		if err != nil {
			return nil, err
		}
		q.Where = append(q.Where, condition(expr))
	}

	if q.Sort, err = sortEntries(req, columns.sortBy, &cfg); err != nil {
		return nil, err
	}
	describeRequest(req, &cfg, q.Sort, searchBy)
	return q, nil
}

func condition(expr sqlExpr) Condition {
	return Condition{SQL: expr.sql, Args: expr.args}
}

// normalizeConfig fills the zero limits with defaults and checks what every request needs.
func normalizeConfig(cfg *Config) error {
	if cfg.DefaultLimit == 0 {
		cfg.DefaultLimit = DefaultLimit
	}
	if cfg.MaxLimit == 0 {
		cfg.MaxLimit = DefaultMaxLimit
	}
	if cfg.DefaultLimit > cfg.MaxLimit {
		cfg.DefaultLimit = cfg.MaxLimit
	}
	if cfg.MaxSearchLength == 0 {
		cfg.MaxSearchLength = DefaultMaxSearchLength
	}
	if cfg.MaxFilterConditions == 0 {
		cfg.MaxFilterConditions = DefaultMaxFilterConditions
	}
	if cfg.MaxFilterValues == 0 {
		cfg.MaxFilterValues = DefaultMaxFilterValues
	}
	if cfg.MaxFilterExpressionComplexity == 0 {
		cfg.MaxFilterExpressionComplexity = DefaultMaxFilterExpressionComplexity
	}
	if cfg.TableName == "" {
		return invalidConfig("tableName is required")
	}
	return nil
}

// requestColumns is a request in the config's terms: columns instead of API fields.
type requestColumns struct {
	sortBy   map[string]Order
	searchBy []string
	filter   map[string]*fieldValues
}

// normalizeRequest fixes the page, the limit and the mode of a request and maps its fields to
// columns. Unknown fields fail with StrictValidate and are dropped without it; malformed values
// and values over the caps fail always.
func normalizeRequest(req *Request, cfg *Config) (requestColumns, error) {
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = cfg.DefaultLimit
	}
	if req.Limit > cfg.MaxLimit {
		req.Limit = cfg.MaxLimit
	}
	// The offset (page-1)*limit must fit a signed integer: GORM takes an int, Postgres a bigint.
	if req.Page-1 > uint64(math.MaxInt)/req.Limit {
		return requestColumns{}, invalidRequest("page %d is too large", req.Page)
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
		return requestColumns{}, invalidRequest("search is longer than %d characters", cfg.MaxSearchLength)
	}

	columns := requestColumns{
		sortBy: make(map[string]Order, len(req.SortBy)),
		filter: make(map[string]*fieldValues, len(req.Filter)),
	}
	for _, field := range slices.Sorted(maps.Keys(req.SortBy)) {
		// The parsers check the order already; requests built in code are checked here, since the
		// order goes into ORDER BY as text.
		order := Order(strings.ToLower(string(req.SortBy[field])))
		if order != Asc && order != Desc {
			return requestColumns{}, invalidRequest("sort order of %s must be %s or %s", field, Asc, Desc)
		}
		column, exists := cfg.FieldColumns[field]
		if !exists {
			if cfg.StrictValidate {
				return requestColumns{}, invalidRequest("sorting by %s is not allowed", field)
			}
			continue
		}
		columns.sortBy[column] = order
	}

	if !cfg.IgnoreSearchByInQueryParam {
		for _, field := range req.SearchBy {
			column, exists := cfg.FieldColumns[field]
			if !exists {
				if cfg.StrictValidate {
					return requestColumns{}, invalidRequest("searching by %s is not allowed", field)
				}
				continue
			}
			columns.searchBy = append(columns.searchBy, column)
		}
	}

	conditions := uint64(0)
	for _, field := range slices.Sorted(maps.Keys(req.Filter)) {
		values := req.Filter[field]
		conditions += uint64(len(values))
		if conditions > cfg.MaxFilterConditions {
			return requestColumns{}, invalidRequest("too many filter values (more than %d)", cfg.MaxFilterConditions)
		}
		column, exists := cfg.FieldColumns[field]
		if !exists {
			if cfg.StrictValidate {
				return requestColumns{}, invalidRequest("unknown filter field %s", field)
			}
			continue
		}
		// Two API fields may map to one column; their values join its chain.
		if input, ok := columns.filter[column]; ok {
			input.values = append(input.values, values...)
		} else {
			columns.filter[column] = &fieldValues{field: field, values: slices.Clone(values)}
		}
	}
	return columns, nil
}
