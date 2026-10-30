package paginate

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

// Response is the {data, meta} answer of a list endpoint; with FormatNestJS its JSON adds links.
type Response[T any] struct {
	Data []T  `json:"data"`
	Meta Meta `json:"meta"`
}

// Meta describes the page and echoes the request in API terms, so a client can render its controls
// from the answer. Copy it whole into other answers: it carries the format, the nestjs-paginate
// view and the links.
type Meta struct {
	// ItemsPerPage is the page size: the limit, or every item for the count only and for every row.
	ItemsPerPage uint64
	// TotalItems is the counted total and TotalPages the pages in it; both are zero when Count is
	// CountNone.
	TotalItems uint64
	TotalPages uint64
	// Count is how TotalItems was found: CountExact, CountEstimate, or CountNone when it was not.
	Count Count
	// CurrentPage is the page number; with a count it is within [1, max(TotalPages, 1)]. It is zero
	// in cursor pagination.
	CurrentPage uint64
	// HasNextPage and HasPreviousPage tell whether there are pages around this one.
	HasNextPage     bool
	HasPreviousPage bool
	// SortBy is the effective sort in API names, the default included, without the tie-breaker.
	SortBy []Sort
	// Search, SearchBy and Filter echo the request.
	Search   string
	SearchBy []string
	Filter   FilterQuery
	// PaginationMode is the mode the request ran in.
	PaginationMode Mode
	// Cursor is the cursor of the request in cursor pagination; NextCursor and PreviousCursor lead to
	// the neighboring pages. An empty NextCursor with HasNextPage set leads to the first page.
	Cursor         string
	NextCursor     string
	PreviousCursor string

	format Format
	nest   *NestJSMeta
	links  *Links
}

// Total is what an executor counted: Items matching rows, found the Count way (CountExact or
// CountEstimate).
type Total struct {
	Items uint64
	Count Count
}

// NewResponse builds the answer to the request of q from what an executor read: the rows, up to
// q.FetchLimit(), and the total it counted, ignored when q.Count is CountNone. In cursor pagination
// it makes the cursors of the neighboring pages from the first and the last row, so the rows must
// carry the sort fields; the error says which one is missing.
func NewResponse[T any](q *Query, rows []T, total Total) (*Response[T], error) {
	req := &q.req
	more := false
	if q.lookahead() && uint64(len(rows)) > q.Limit {
		more = true
		rows = rows[:q.Limit]
	}
	if rows == nil {
		rows = []T{}
	}
	if q.backward {
		slices.Reverse(rows)
	}

	meta := Meta{
		SortBy:         q.view.sort,
		Search:         req.Search,
		SearchBy:       req.SearchBy,
		Filter:         req.Filter,
		PaginationMode: req.Mode,
		format:         q.view.format,
	}
	switch {
	case req.Mode == ModeCount:
		meta.Count = total.Count
		meta.TotalItems, meta.ItemsPerPage = total.Items, total.Items
		meta.TotalPages, meta.CurrentPage = 1, 1
	case req.Mode == ModeDisabled:
		n := uint64(len(rows))
		meta.Count = CountExact
		meta.TotalItems, meta.ItemsPerPage = n, n
		meta.TotalPages, meta.CurrentPage = 1, 1
	case q.cursor != nil:
		if err := cursorMeta(&meta, q, rows, more); err != nil {
			return nil, err
		}
	case q.Count == CountNone:
		meta.Count = CountNone
		meta.ItemsPerPage = q.Limit
		meta.CurrentPage = req.Page
		meta.HasNextPage = more
		meta.HasPreviousPage = req.Page > 1
	default:
		meta.Count = total.Count
		meta.ItemsPerPage = q.Limit
		meta.TotalItems = total.Items
		meta.TotalPages = total.Items / q.Limit
		if total.Items%q.Limit != 0 {
			meta.TotalPages++
		}
		meta.CurrentPage = max(1, min(req.Page, meta.TotalPages))
		meta.HasNextPage = meta.CurrentPage < meta.TotalPages
		meta.HasPreviousPage = meta.CurrentPage > 1
	}
	presentResponse(&meta, q, len(rows))
	return &Response[T]{Data: rows, Meta: meta}, nil
}

// cursorMeta fills the meta of a cursor page. Paging forward, there are rows after the page when
// the executor read more than the limit, and rows before it when the request came with a cursor
// (the row it marks); paging back, the other way round. An empty page links back through the
// request's own cursor: forward, to the rows up to and including the marked one; back, to the
// first page.
func cursorMeta[T any](meta *Meta, q *Query, rows []T, more bool) error {
	req := &q.req
	meta.Count = CountNone
	meta.ItemsPerPage = q.Limit
	meta.Cursor = req.Cursor
	if q.backward {
		meta.HasPreviousPage, meta.HasNextPage = more, true
	} else {
		meta.HasPreviousPage, meta.HasNextPage = req.Cursor != "", more
	}

	var err error
	switch {
	case len(rows) != 0:
		if meta.HasNextPage {
			if meta.NextCursor, err = q.cursor.encode(&rows[len(rows)-1], cursorAfter); err != nil {
				return err
			}
		}
		if meta.HasPreviousPage {
			meta.PreviousCursor, err = q.cursor.encode(&rows[0], cursorBefore)
		}
	case !q.backward && meta.HasPreviousPage:
		meta.PreviousCursor, err = q.cursor.encodeValues(q.cursor.boundary, cursorBackThrough)
	}
	return err
}

// Map converts the rows of an answer and keeps its meta, links included:
//
//	page, err := gormx.Paginate[User](ctx, *req, usersEndpoint, db)
//	...
//	writeJSON(w, http.StatusOK, paginate.Map(page, toUserDTO))
func Map[T, U any](r *Response[T], convert func(T) U) *Response[U] {
	data := make([]U, len(r.Data))
	for i, row := range r.Data {
		data[i] = convert(row)
	}
	return &Response[U]{Data: data, Meta: r.Meta}
}

// ExplainPrefix turns a query into the statement whose output EstimateFromPlan reads.
const ExplainPrefix = "EXPLAIN (FORMAT JSON) "

// EstimateFromPlan reads the planner's estimate of the rows a query returns from the output of
// EXPLAIN (FORMAT JSON) <query>, for CountEstimate.
func EstimateFromPlan(plan []byte) (uint64, error) {
	var parsed []struct {
		Plan struct {
			Rows *float64 `json:"Plan Rows"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &parsed); err != nil {
		return 0, fmt.Errorf("paginate: read EXPLAIN output: %w", err)
	}
	if len(parsed) == 0 || parsed[0].Plan.Rows == nil || math.IsNaN(*parsed[0].Plan.Rows) || *parsed[0].Plan.Rows < 0 {
		return 0, fmt.Errorf("paginate: read EXPLAIN output: no row estimate in %.80s", plan)
	}
	return uint64(min(math.Round(*parsed[0].Plan.Rows), math.MaxUint64)), nil
}
