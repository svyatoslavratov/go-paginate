package paginate

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// ParseRequest reads the query string of a net/http request (chi, gorilla, gin's c.Request, any
// router) into a Request. It fails only on a malformed sortBy; fields, operators, values and limits
// are checked by Endpoint.Prepare.
func ParseRequest(r *http.Request) (*Request, error) {
	return ParseQuery(r.URL.Query(), r.URL.EscapedPath())
}

// ParseQuery reads the nestjs-paginate query parameters: page, limit, sortBy, search, searchBy,
// filter.<field>, filter=<expression>, cursor, withDeleted, and paginationMode. path is the request
// path the links of the answer start with; it may be empty.
func ParseQuery(query url.Values, path string) (*Request, error) {
	req := &Request{Path: path}

	limit := query.Get("limit")
	req.Limit = parseUint(limit)
	req.Page = parseUint(query.Get("page"))

	sortBy, err := parseSortBy(query["sortBy"])
	if err != nil {
		return nil, err
	}
	req.SortBy = sortBy

	req.Search = query.Get("search")
	req.SearchBy = query["searchBy"]
	req.Filter = parseFilterParams(query)
	req.FilterExpression = joinFilterExpressions(query["filter"])
	req.Cursor = query.Get("cursor")
	req.WithDeleted, _ = strconv.ParseBool(query.Get("withDeleted"))

	mode := ModeDefault
	if value := query.Get("paginationMode"); value != "" {
		mode = parseMode(value)
	} else if n, err := strconv.ParseInt(limit, 10, 64); err == nil {
		// nestjs-paginate asks for the count only with limit=0 and for every row with limit=-1.
		switch n {
		case 0:
			mode = ModeCount
		case -1:
			mode = ModeDisabled
		}
	}
	req.requestMode(mode)
	return req, nil
}

// parseUint reads a page or a limit: garbage is zero, a number too large is the largest one, which
// Prepare refuses as a page and caps as a limit.
func parseUint(value string) uint64 {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0
	}
	return parsed
}

// parseSortBy reads repeated sortBy=field:order values in request order, which is the sort
// priority; a repeated field keeps its first position and its last order.
func parseSortBy(values []string) ([]Sort, error) {
	sortBy := make([]Sort, 0, len(values))
	for _, value := range values {
		field, rawOrder, found := strings.Cut(value, ":")
		if !found || strings.Contains(rawOrder, ":") {
			return nil, invalidRequest(CodeInvalidSort, "", "sortBy %s must be field:ASC or field:DESC", quoteValue(value))
		}
		order, ok := parseOrder(rawOrder)
		if !ok {
			return nil, invalidRequest(CodeInvalidSort, field, "sort order of %s must be %s or %s", field, Asc, Desc)
		}
		if i := slices.IndexFunc(sortBy, func(s Sort) bool { return s.Field == field }); i >= 0 {
			sortBy[i].Order = order
			continue
		}
		sortBy = append(sortBy, Sort{Field: field, Order: order})
	}
	return sortBy, nil
}

// parseFilterParams collects filter.<field>=<value> parameters; empty values are dropped.
func parseFilterParams(query url.Values) FilterQuery {
	filter := make(FilterQuery)
	for name, values := range query {
		field, ok := strings.CutPrefix(name, "filter.")
		if !ok {
			continue
		}
		for _, value := range values {
			if value != "" {
				filter[field] = append(filter[field], value)
			}
		}
	}
	return filter
}

// joinFilterExpressions ANDs repeated filter=<expression> parameters; empty ones are ignored.
func joinFilterExpressions(values []string) string {
	expressions := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			expressions = append(expressions, value)
		}
	}
	switch len(expressions) {
	case 0:
		return ""
	case 1:
		return expressions[0]
	}
	return "(" + strings.Join(expressions, ") AND (") + ")"
}
