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
	// FormatDefault is this package's shape: meta echoes the request (sortBy as a map, filter
	// values as arrays) and has paginationMode; there are no links.
	FormatDefault Format = ""
	// FormatNestJS is nestjs-paginate's shape: meta.sortBy holds the effective sort as
	// [field, "ASC"|"DESC"] pairs, meta.filter a string per single value, search and searchBy only
	// with a search, and links point to the first, previous, current, next and last pages.
	FormatNestJS Format = "nestjs"
)

// Links are the URLs of the neighboring pages, as nestjs-paginate builds them: first and previous
// are missing on the first page, next and last on the last one.
type Links struct {
	First    string `json:"first,omitempty"`
	Previous string `json:"previous,omitempty"`
	Current  string `json:"current,omitempty"`
	Next     string `json:"next,omitempty"`
	Last     string `json:"last,omitempty"`
}

// NestJSMeta is the meta in nestjs-paginate's shape. For the count only (limit=0) it reports one
// page with every item, for disabled pagination one page with the rows returned.
type NestJSMeta struct {
	ItemsPerPage uint64                        `json:"itemsPerPage"`
	TotalItems   uint64                        `json:"totalItems"`
	CurrentPage  uint64                        `json:"currentPage"`
	TotalPages   uint64                        `json:"totalPages"`
	SortBy       [][2]string                   `json:"sortBy"`
	Search       string                        `json:"search,omitempty"`
	SearchBy     []string                      `json:"searchBy,omitempty"`
	Filter       map[string]NestJSFilterValues `json:"filter,omitempty"`
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

// NestJS returns the meta in nestjs-paginate's shape. Answers built from a prepared request have
// it whatever Format says, so a handler that answers with its own API types can use it; ok is
// false for a meta built by hand.
func (m Meta) NestJS() (meta NestJSMeta, ok bool) {
	if m.nest == nil {
		return NestJSMeta{}, false
	}
	return *m.nest, true
}

// Links returns the links to the neighboring pages. They are empty unless the request came through
// a parser, which records its path.
func (m Meta) Links() Links {
	if m.links == nil {
		return Links{}
	}
	return *m.links
}

// MarshalJSON writes the meta in the shape of Format.
func (m Meta) MarshalJSON() ([]byte, error) {
	if m.format == FormatNestJS && m.nest != nil {
		return marshalJSON(m.nest)
	}
	type plainMeta Meta
	return marshalJSON(plainMeta(m))
}

// MarshalJSON writes the answer in the shape of Format; the nestjs shape adds links.
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

// responseView keeps what an answer needs from a prepared request and its config.
type responseView struct {
	format  Format
	baseURL string
	// sort is the effective sort with API field names and ASC/DESC, without the tie-breaker.
	sort [][2]string
	// sortInLinks is false when a sort column has no API field: links leave sortBy out, so the
	// server applies the same defaults again.
	sortInLinks bool
	// searchBy are the API fields the search covers.
	searchBy []string
	// searchByInLinks is set when the client narrowed the search itself.
	searchByInLinks bool
}

// describeRequest records in req what NewResponse needs for the meta and the links: the effective
// sort and the search columns in API names.
func describeRequest(req *Request, cfg *Config, entries []SortEntry, searchBy []string) {
	byColumn := make(map[string]string, len(cfg.FieldColumns))
	for _, field := range slices.Sorted(maps.Keys(cfg.FieldColumns)) {
		column := cfg.FieldColumns[field]
		if _, seen := byColumn[column]; !seen {
			byColumn[column] = field
		}
	}
	// Prefer the names the client used.
	clientNames := func(fields []string) map[string]string {
		names := make(map[string]string, len(fields))
		for _, field := range fields {
			if column, ok := cfg.FieldColumns[field]; ok {
				if _, seen := names[column]; !seen {
					names[column] = field
				}
			}
		}
		return names
	}
	apiName := func(column string, client map[string]string) (string, bool) {
		if field, ok := client[column]; ok {
			return field, true
		}
		field, ok := byColumn[column]
		return field, ok
	}

	view := &responseView{
		format:          cfg.Format,
		baseURL:         strings.TrimSuffix(cfg.LinksBaseURL, "/"),
		sort:            make([][2]string, 0, len(entries)),
		sortInLinks:     true,
		searchByInLinks: len(req.SearchBy) != 0 && !cfg.IgnoreSearchByInQueryParam,
	}
	sortKeys := req.SortKeys
	if len(sortKeys) == 0 {
		sortKeys = slices.Sorted(maps.Keys(req.SortBy))
	}
	sortNames := clientNames(sortKeys)
	for _, entry := range entries {
		field, ok := apiName(entry.Column, sortNames)
		if !ok {
			field, view.sortInLinks = entry.Column, false
		}
		view.sort = append(view.sort, [2]string{field, strings.ToUpper(string(entry.Order))})
	}
	searchNames := clientNames(req.SearchBy)
	for _, column := range searchBy {
		field, ok := apiName(column, searchNames)
		if !ok {
			field = column
		}
		view.searchBy = append(view.searchBy, field)
	}
	req.view = view
}

// presentResponse fills the nestjs meta and the links of an answer built from a prepared request.
func presentResponse(meta *Meta, req *Request, dataLen int, totalItems uint64) {
	view := req.view
	meta.format = view.format

	nest := &NestJSMeta{SortBy: view.sort}
	switch {
	case req.Mode == ModeCount:
		nest.ItemsPerPage, nest.TotalItems, nest.CurrentPage, nest.TotalPages = totalItems, totalItems, 1, 1
	case req.Mode == ModeDisabled || req.Limit == 0:
		nest.ItemsPerPage, nest.TotalItems, nest.CurrentPage, nest.TotalPages = uint64(dataLen), uint64(dataLen), 1, 1
	default:
		nest.ItemsPerPage, nest.TotalItems = req.Limit, totalItems
		nest.TotalPages = totalItems / req.Limit
		if totalItems%req.Limit != 0 {
			nest.TotalPages++
		}
		nest.CurrentPage = max(1, min(req.Page, nest.TotalPages))
	}
	if req.Search != "" {
		nest.Search = req.Search
		nest.SearchBy = view.searchBy
	}
	if len(req.Filter) != 0 {
		nest.Filter = make(map[string]NestJSFilterValues, len(req.Filter))
		for field, values := range req.Filter {
			nest.Filter[field] = NestJSFilterValues(values)
		}
	}
	meta.nest = nest

	if req.Path != "" {
		meta.links = buildLinks(req, view, nest)
	}
}

// buildLinks builds the links the nestjs-paginate way: page first, then limit, sortBy, search,
// searchBy and the filters, so following a link repeats the request on another page.
func buildLinks(req *Request, view *responseView, nest *NestJSMeta) *Links {
	var query strings.Builder
	limit := strconv.FormatUint(req.Limit, 10)
	switch req.Mode {
	case ModeCount:
		limit = "0"
	case ModeDisabled:
		limit = "-1"
	}
	query.WriteString("&limit=" + limit)
	if view.sortInLinks {
		for _, pair := range view.sort {
			query.WriteString("&sortBy=" + escapeLinkValue(pair[0]+":"+pair[1]))
		}
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
	rest := query.String()
	link := func(page uint64) string {
		return view.baseURL + req.Path + "?page=" + strconv.FormatUint(page, 10) + rest
	}

	links := &Links{Current: link(nest.CurrentPage)}
	if nest.CurrentPage > 1 {
		links.First = link(1)
		links.Previous = link(nest.CurrentPage - 1)
	}
	if nest.CurrentPage < nest.TotalPages {
		links.Next = link(nest.CurrentPage + 1)
	}
	if nest.CurrentPage != nest.TotalPages && nest.TotalItems != 0 {
		links.Last = link(nest.TotalPages)
	}
	return links
}

// linkUnescaper keeps characters of the filter syntax readable in links; they are valid in a query.
var linkUnescaper = strings.NewReplacer("%24", "$", "%3A", ":", "%2C", ",", "%28", "(", "%29", ")")

func escapeLinkValue(value string) string {
	return linkUnescaper.Replace(url.QueryEscape(value))
}
