package paginate

// FilterQuery maps an API field to its filter.<field> values.
type FilterQuery map[string][]string

// Mode is what a request returns: a page, the count only, or every row.
type Mode string

const (
	// ModeDefault returns a page and the total count.
	ModeDefault Mode = "default"
	// ModeCount returns the total count without rows (?limit=0 or ?paginationMode=count).
	ModeCount Mode = "count"
	// ModeDisabled returns every matching row and the total count (?limit=-1 or
	// ?paginationMode=disabled). Clients get it only when Config.AllowDisabledPagination is set.
	ModeDisabled Mode = "disabled"
)

// parseMode reads the paginationMode parameter; an unknown mode is the default one.
func parseMode(value string) Mode {
	switch mode := Mode(value); mode {
	case ModeCount, ModeDisabled:
		return mode
	}
	return ModeDefault
}

// Request is a parsed list request, in the client's terms: API field names, raw filter values.
// The parsers fill it; Prepare checks it against a Config.
type Request struct {
	// Page is the page number, from 1; zero means the first page.
	Page uint64 `json:"page,omitempty"`
	// Limit is the page size; zero means the default.
	Limit uint64 `json:"limit,omitempty"`
	// SortBy is sortBy as the client sent it: API field to direction.
	SortBy SortBy `json:"sortBy,omitempty"`
	// SortKeys is the order of the sortBy keys, the first is the main key. SortBy is a map and
	// keeps no order; without this list the keys follow SortableColumns.
	SortKeys []string `json:"sortKeys,omitempty"`
	// Search is the search term.
	Search string `json:"search,omitempty"`
	// SearchBy are the API fields the client narrowed the search to.
	SearchBy []string `json:"searchBy,omitempty"`
	// Filter holds the filter.<field> values by API field.
	Filter FilterQuery `json:"filter,omitempty"`
	// FilterExpression is the filter=<expression> parameter, ANDed with Filter:
	// (status=$eq:paid OR total=$gte:1000) AND NOT comment=$null.
	FilterExpression string `json:"filterExpression,omitempty"`
	// Mode is the mode the request runs in. A mode server code sets here is applied as is.
	Mode Mode `json:"mode,omitempty"`
	// RequestedMode is the mode the client asked for. Count takes effect at once; disabled waits
	// for Config.AllowDisabledPagination and for Mode to be unset.
	RequestedMode Mode `json:"requestedMode,omitempty"`
	// WithDeleted asks for soft-deleted rows too, honored with Config.AllowWithDeletedInQuery.
	WithDeleted bool `json:"withDeleted,omitempty"`
	// Path is the path of the HTTP request, set by the parsers; links start with it.
	Path string `json:"path,omitempty"`

	// view keeps what the answer needs from Prepare.
	view *responseView
}

// requestMode records the mode a client asked for: count at once, disabled after the config check.
func (r *Request) requestMode(mode Mode) {
	r.RequestedMode = mode
	if mode != ModeDisabled {
		r.Mode = mode
	}
}

// hasFilters reports filter values or a filter expression in the request.
func (r *Request) hasFilters() bool {
	return len(r.Filter) != 0 || r.FilterExpression != ""
}
