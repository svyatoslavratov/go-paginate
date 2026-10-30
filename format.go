package paginate

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Format is the JSON shape of a Response.
type Format string

const (
	// FormatDefault is this package's shape: meta has the totals (when counted), hasNextPage and
	// hasPreviousPage, estimated for estimates, the effective sort as [field, "ASC"|"DESC"] pairs, the
	// request's search, searchBy and filter (values as arrays), paginationMode, and the cursors in
	// cursor pagination. There are no links.
	FormatDefault Format = ""
	// FormatNestJS is nestjs-paginate's shape: meta has the effective sort as pairs, filter values
	// as a string per single value, search and searchBy only with a search, no totals in cursor
	// pagination; links point to the neighboring pages.
	FormatNestJS Format = "nestjs"
)

// Links are the URLs of the neighboring pages, as nestjs-paginate builds them: first and previous
// are missing on the first page, next on the last one, last when it is unknown. Cursor pagination
// has previous, current and next.
type Links struct {
	First    string `json:"first,omitempty"`
	Previous string `json:"previous,omitempty"`
	Current  string `json:"current,omitempty"`
	Next     string `json:"next,omitempty"`
	Last     string `json:"last,omitempty"`
}

// NestJSMeta is the meta in nestjs-paginate's shape. The count only (limit=0) and every row
// (limit=-1) are one page with every item. Without a count there is no totalItems and totalPages;
// cursor pagination has no currentPage either, and itemsPerPage is the rows of the page.
type NestJSMeta struct {
	ItemsPerPage uint64                        `json:"itemsPerPage"`
	TotalItems   *uint64                       `json:"totalItems,omitempty"`
	CurrentPage  *uint64                       `json:"currentPage,omitempty"`
	TotalPages   *uint64                       `json:"totalPages,omitempty"`
	SortBy       []Sort                        `json:"sortBy"`
	Search       string                        `json:"search,omitempty"`
	SearchBy     []string                      `json:"searchBy,omitempty"`
	Filter       map[string]NestJSFilterValues `json:"filter,omitempty"`
	Cursor       string                        `json:"cursor,omitempty"`
}

// NestJSFilterValues are the values of one filter field: JSON has a string for one value and an
// array for several, as nestjs-paginate echoes the query.
type NestJSFilterValues []string

// MarshalJSON writes one value as a string, several as an array.
func (v NestJSFilterValues) MarshalJSON() ([]byte, error) {
	if len(v) == 1 {
		return marshalJSON(v[0])
	}
	return marshalJSON([]string(v))
}

// NestJS returns the meta in nestjs-paginate's shape. Answers built by NewResponse have it whatever
// the format, so a handler that answers with its own API types can use it; ok is false for a meta
// built by hand.
func (m Meta) NestJS() (meta NestJSMeta, ok bool) {
	if m.nest == nil {
		return NestJSMeta{}, false
	}
	return *m.nest, true
}

// Links returns the links to the neighboring pages, in any format, for example for a Link header.
// They are empty unless the request came through a parser, which records its path.
func (m Meta) Links() Links {
	if m.links == nil {
		return Links{}
	}
	return *m.links
}

// defaultMeta is the JSON of a Meta in FormatDefault.
type defaultMeta struct {
	ItemsPerPage    uint64      `json:"itemsPerPage"`
	TotalItems      *uint64     `json:"totalItems,omitempty"`
	TotalPages      *uint64     `json:"totalPages,omitempty"`
	Estimated       bool        `json:"estimated,omitempty"`
	CurrentPage     *uint64     `json:"currentPage,omitempty"`
	HasNextPage     bool        `json:"hasNextPage"`
	HasPreviousPage bool        `json:"hasPreviousPage"`
	SortBy          []Sort      `json:"sortBy"`
	Search          string      `json:"search"`
	SearchBy        []string    `json:"searchBy"`
	Filter          FilterQuery `json:"filter"`
	PaginationMode  Mode        `json:"paginationMode"`
	Cursor          string      `json:"cursor,omitempty"`
	NextCursor      string      `json:"nextCursor,omitempty"`
	PreviousCursor  string      `json:"previousCursor,omitempty"`
}

// MarshalJSON writes the meta in the shape of its format.
func (m Meta) MarshalJSON() ([]byte, error) {
	if m.format == FormatNestJS && m.nest != nil {
		return marshalJSON(m.nest)
	}
	out := defaultMeta{
		ItemsPerPage:    m.ItemsPerPage,
		Estimated:       m.Count == CountEstimate,
		HasNextPage:     m.HasNextPage,
		HasPreviousPage: m.HasPreviousPage,
		SortBy:          m.SortBy,
		Search:          m.Search,
		SearchBy:        m.SearchBy,
		Filter:          m.Filter,
		PaginationMode:  m.PaginationMode,
		Cursor:          m.Cursor,
		NextCursor:      m.NextCursor,
		PreviousCursor:  m.PreviousCursor,
	}
	if m.Count != CountNone {
		out.TotalItems, out.TotalPages = &m.TotalItems, &m.TotalPages
	}
	if m.CurrentPage != 0 {
		out.CurrentPage = &m.CurrentPage
	}
	if out.SortBy == nil {
		out.SortBy = []Sort{}
	}
	if out.SearchBy == nil {
		out.SearchBy = []string{}
	}
	if out.Filter == nil {
		out.Filter = FilterQuery{}
	}
	if out.PaginationMode == "" {
		out.PaginationMode = ModeDefault
	}
	return marshalJSON(out)
}

// MarshalJSON writes the answer in the shape of its format; the nestjs shape adds links.
func (r Response[T]) MarshalJSON() ([]byte, error) {
	if r.Meta.format == FormatNestJS && r.Meta.nest != nil {
		return marshalJSON(struct {
			Data  []T   `json:"data"`
			Meta  Meta  `json:"meta"`
			Links Links `json:"links"`
		}{r.Data, r.Meta, r.Meta.Links()})
	}
	return marshalJSON(struct {
		Data []T  `json:"data"`
		Meta Meta `json:"meta"`
	}{r.Data, r.Meta})
}

// marshalJSON encodes without HTML escaping: the caller's encoder still escapes "&", "<" and ">"
// unless it is told not to.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// presentResponse fills the nestjs-paginate view and the links of an answer.
func presentResponse(meta *Meta, q *Query, rows int) {
	req := &q.req
	nest := &NestJSMeta{ItemsPerPage: meta.ItemsPerPage, SortBy: meta.SortBy}
	items, page, pages := meta.TotalItems, meta.CurrentPage, meta.TotalPages
	switch {
	case q.cursor != nil:
		nest.ItemsPerPage = uint64(rows)
		nest.Cursor = req.Cursor
	case meta.Count == CountNone:
		nest.CurrentPage = &page
	default:
		nest.TotalItems, nest.CurrentPage, nest.TotalPages = &items, &page, &pages
	}
	if nest.SortBy == nil {
		nest.SortBy = []Sort{}
	}
	if req.Search != "" {
		nest.Search = req.Search
		nest.SearchBy = q.view.searchBy
	}
	if len(req.Filter) != 0 {
		nest.Filter = make(map[string]NestJSFilterValues, len(req.Filter))
		for field, values := range req.Filter {
			nest.Filter[field] = NestJSFilterValues(values)
		}
	}
	meta.nest = nest

	if req.Path != "" {
		meta.links = buildLinks(meta, q)
	}
}

// buildLinks builds the links the nestjs-paginate way: page first, then limit, sortBy, search,
// searchBy and the filters, so following a link repeats the request on another page; cursor
// links end with the cursor instead.
func buildLinks(meta *Meta, q *Query) *Links {
	req, view := &q.req, q.view
	var query strings.Builder
	limit := strconv.FormatUint(req.Limit, 10)
	switch req.Mode {
	case ModeCount:
		limit = "0"
	case ModeDisabled:
		limit = "-1"
	}
	query.WriteString("limit=" + limit)
	for _, key := range view.sort {
		query.WriteString("&sortBy=" + escapeLinkValue(key.Field+":"+string(key.Order)))
	}
	if req.Search != "" {
		query.WriteString("&search=" + escapeLinkValue(req.Search))
	}
	if view.searchByInLinks {
		for _, field := range view.searchBy {
			query.WriteString("&searchBy=" + escapeLinkValue(field))
		}
	}
	for _, field := range slices.Sorted(maps.Keys(req.Filter)) {
		for _, value := range req.Filter[field] {
			query.WriteString("&" + escapeLinkValue("filter."+field) + "=" + escapeLinkValue(value))
		}
	}
	if req.FilterExpression != "" {
		query.WriteString("&filter=" + escapeLinkValue(req.FilterExpression))
	}
	if req.WithDeleted {
		query.WriteString("&withDeleted=true")
	}
	base, rest := view.baseURL+linkPath(req.Path)+"?", query.String()

	links := &Links{}
	if q.cursor != nil {
		link := func(cursor string) string {
			if cursor == "" {
				return base + rest
			}
			return base + rest + "&cursor=" + escapeLinkValue(cursor)
		}
		links.Current = link(req.Cursor)
		if meta.HasPreviousPage {
			links.Previous = link(meta.PreviousCursor)
		}
		if meta.HasNextPage {
			links.Next = link(meta.NextCursor)
		}
		return links
	}

	link := func(page uint64) string {
		return base + "page=" + strconv.FormatUint(page, 10) + "&" + rest
	}
	links.Current = link(meta.CurrentPage)
	if meta.CurrentPage > 1 {
		links.First = link(1)
		links.Previous = link(meta.CurrentPage - 1)
	}
	if meta.HasNextPage {
		links.Next = link(meta.CurrentPage + 1)
	}
	// An estimated last page may hold no rows, an uncounted one is unknown.
	if meta.Count == CountExact && meta.CurrentPage != meta.TotalPages && meta.TotalItems != 0 {
		links.Last = link(meta.TotalPages)
	}
	return links
}

// linkPath keeps a link on its host: a path that starts with // would read as a link to another
// host (//evil.example/users), so the slashes collapse into one.
func linkPath(path string) string {
	if strings.HasPrefix(path, "//") {
		return "/" + strings.TrimLeft(path, "/")
	}
	return path
}

// linkUnescaper keeps characters of the filter syntax readable in links; they are valid in a query.
var linkUnescaper = strings.NewReplacer("%24", "$", "%3A", ":", "%2C", ",", "%28", "(", "%29", ")")

func escapeLinkValue(value string) string {
	return linkUnescaper.Replace(url.QueryEscape(value))
}
