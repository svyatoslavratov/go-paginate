package paginate

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ParseRequest reads the query string of a net/http request (chi, gorilla, gin's c.Request, any
// router) into a Request. It fails only on a malformed sortBy; fields, operators and limits are
// checked against the Config by Prepare.
func ParseRequest(r *http.Request) (*Request, error) {
	return ParseQuery(r.URL.Query(), r.URL.EscapedPath())
}

// ParseQuery reads the nestjs-paginate query parameters: page, limit, sortBy, search, searchBy,
// filter.<field>, filter=<expression>, withDeleted, and paginationMode. path is the request path
// the links of the answer start with; it may be empty.
func ParseQuery(query url.Values, path string) (*Request, error) {
	req := &Request{Path: path}

	limit := query.Get("limit")
	req.Limit = parseUint(limit)
	req.Page = parseUint(query.Get("page"))

	sortBy, sortKeys, err := parseSortBy(query["sortBy"])
	if err != nil {
		return nil, err
	}
	req.SortBy = sortBy
	req.SortKeys = sortKeys

	req.Search = query.Get("search")
	req.SearchBy = query["searchBy"]
	req.Filter = parseFilterParams(query)
	req.FilterExpression = joinFilterExpressions(query["filter"])
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

func parseUint(value string) uint64 {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

// parseSortBy reads repeated sortBy=field:order values into a map and the list of keys in request
// order, which is the sort priority; a repeated key keeps its first position and the last order.
func parseSortBy(values []string) (SortBy, []string, error) {
	sortBy := make(SortBy, len(values))
	keys := make([]string, 0, len(values))
	for _, value := range values {
		field, order, found := strings.Cut(value, ":")
		if !found || strings.Contains(order, ":") {
			return nil, nil, invalidRequest("sortBy %q must be field:asc or field:desc", value)
		}
		order = strings.ToLower(order)
		if order != string(Asc) && order != string(Desc) {
			return nil, nil, invalidRequest("sort order of %s must be %s or %s", field, Asc, Desc)
		}
		if _, seen := sortBy[field]; !seen {
			keys = append(keys, field)
		}
		sortBy[field] = Order(order)
	}
	return sortBy, keys, nil
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
