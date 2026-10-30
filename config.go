package paginate

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// FilterableColumns maps a column to the operators clients may use on it. $eq is always allowed:
// an empty list allows $eq only, AllOperators allows every operator.
type FilterableColumns map[string][]Operator

// Config is the server side of a paginated endpoint: what clients may sort, search and filter by,
// and the limits. Columns are SQL expressions as the query names them ("users.created_at",
// "u.name", "COALESCE(p.nickname, u.name)"); clients name API fields, mapped to columns by
// FieldColumns. Zero numeric fields take the Default* constants. Build a Config once, not per
// request.
type Config struct {
	// TableName names the table; the GORM builder qualifies columns with it. Required.
	TableName string

	// FieldColumns maps the API fields of sortBy, searchBy and filter to columns:
	// {"createdAt": "users.created_at"}.
	FieldColumns map[string]string

	// SortableColumns are the columns clients may sort by. Required: the first one is the sort of
	// last resort.
	SortableColumns []string

	// DefaultSortBy is the sort of a request without sortBy. Empty: the first sortable column
	// ascending.
	DefaultSortBy SortBy

	// DefaultSortKeys is the order of the DefaultSortBy keys, the first one is the main key. The
	// GORM builder fills it; keys missing here go after it alphabetically.
	DefaultSortKeys []string

	// NullSort puts NULL values first or last in every sort key; zero keeps the database default
	// (NULLS LAST for ASC, NULLS FIRST for DESC in Postgres).
	NullSort NullSort

	// TieBreakColumn is a unique column (usually the primary key, "users.id") appended as the last
	// ORDER BY key, so rows with equal sort values do not move between LIMIT/OFFSET pages. Empty:
	// no tie-breaker.
	TieBreakColumn string

	// SearchableColumns are matched against ?search= with a case-insensitive substring match;
	// ?searchBy= narrows them.
	SearchableColumns []string

	// MultiWordSearch splits the search term on whitespace: every word must match some searchable
	// column ("big red" finds "red big box"). Off: the term is matched as a whole.
	MultiWordSearch bool

	// IgnoreSearchByInQueryParam ignores ?searchBy=: the search always covers SearchableColumns.
	IgnoreSearchByInQueryParam bool

	// MaxSearchLength caps the search term, in characters. Default DefaultMaxSearchLength.
	MaxSearchLength uint64

	// FilterableColumns are the columns clients may filter by, with the operators allowed on each:
	//
	//	FilterableColumns{
	//		"users.status": {},                    // $eq only
	//		"users.age":    {OpGt, OpLt},
	//		"users.name":   AllOperators,
	//	}
	FilterableColumns FilterableColumns

	// CompareAsText makes $eq and $in compare CAST(column AS TEXT). Off (the default): the value is
	// compared with the column's own type, which uses indexes and matches numbers and dates by
	// value; a value of the wrong type ("abc" for an integer) is a 400.
	CompareAsText bool

	// MaxFilterConditions caps the number of filter.<field> values in one request.
	// Default DefaultMaxFilterConditions.
	MaxFilterConditions uint64

	// MaxFilterValues caps the values of one $in, $contains or $btw list.
	// Default DefaultMaxFilterValues.
	MaxFilterValues uint64

	// MaxFilterExpressionComplexity caps the nodes of a filter=<expression>: terms, AND, OR, NOT and
	// parenthesized groups. Default DefaultMaxFilterExpressionComplexity.
	MaxFilterExpressionComplexity uint64

	// DefaultLimit is the page size of a request without limit. Default DefaultLimit.
	DefaultLimit uint64

	// MaxLimit caps the page size a client may ask for. Default DefaultMaxLimit.
	MaxLimit uint64

	// AllowDisabledPagination lets clients ask for every row at once with ?limit=-1 or
	// ?paginationMode=disabled. Off: such requests get the first page. A mode the server sets in
	// Request.Mode itself is applied regardless.
	AllowDisabledPagination bool

	// AllowWithDeletedInQuery lets clients ask for soft-deleted rows with ?withDeleted=true. The GORM
	// executor runs such requests Unscoped.
	AllowWithDeletedInQuery bool

	// StrictValidate answers unknown fields and operators that are not allowed with
	// ErrInvalidRequest; off, they are dropped. NewConfig turns it on. Malformed values, values over
	// the caps and filter expressions fail either way.
	StrictValidate bool

	// Format is the JSON shape of the answer: this package's (FormatDefault) or nestjs-paginate's
	// (FormatNestJS), with the effective sort in meta.sortBy and links to the neighboring pages.
	Format Format

	// LinksBaseURL prefixes the links of the answer: empty gives relative links
	// ("/users?page=2&limit=20"), "https://api.example.com" absolute ones, "/api" restores a prefix
	// a proxy strips. The request's Host header is never used, so a client cannot put its own host
	// into the links.
	LinksBaseURL string
}

// NewConfig starts a config with the default limits and strict validation.
func NewConfig(tableName string, sortableColumns []string, fieldColumns map[string]string) *Config {
	return &Config{
		TableName:         tableName,
		FieldColumns:      fieldColumns,
		SortableColumns:   sortableColumns,
		DefaultSortBy:     make(SortBy),
		FilterableColumns: make(FilterableColumns),
		DefaultLimit:      DefaultLimit,
		MaxLimit:          DefaultMaxLimit,
		StrictValidate:    true,
	}
}

// Validate reports every problem of the config at once: no table or sortable columns, sortable,
// searchable or filterable columns no API field maps to (clients cannot reach them), unknown
// operators, orders and formats. Requests check only what they need, so a broken config fails one
// request at a time; call Validate in a unit test or at startup.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	if c.TableName == "" {
		add("tableName is required")
	}
	if len(c.SortableColumns) == 0 {
		add("sortableColumns is required")
	}
	if c.MaxLimit != 0 && c.DefaultLimit > c.MaxLimit {
		add("defaultLimit %d exceeds maxLimit %d", c.DefaultLimit, c.MaxLimit)
	}
	if c.Format != FormatDefault && c.Format != FormatNestJS {
		add("unknown format %q", c.Format)
	}

	mapped := make(map[string]bool, len(c.FieldColumns))
	for _, column := range c.FieldColumns {
		mapped[column] = true
	}
	for _, column := range c.SortableColumns {
		if !mapped[column] {
			add("sortable column %s: no API field maps to it", column)
		}
	}
	for _, column := range c.SearchableColumns {
		if !mapped[column] {
			add("searchable column %s: no API field maps to it", column)
		}
	}
	for _, column := range slices.Sorted(maps.Keys(c.FilterableColumns)) {
		if !mapped[column] {
			add("filterable column %s: no API field maps to it", column)
		}
		for _, operator := range c.FilterableColumns[column] {
			if !isOperator(string(operator)) && !isSuffix(string(operator)) && !isComparator(string(operator)) {
				add("filterable column %s: unknown operator %s", column, operator)
			}
		}
	}
	for _, column := range slices.Sorted(maps.Keys(c.DefaultSortBy)) {
		if order := c.DefaultSortBy[column]; order != Asc && order != Desc {
			add("default sort %s: order must be %s or %s, got %q", column, Asc, Desc, order)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return &Error{Message: errors.Join(errs...).Error(), Err: ErrInvalidConfig}
}
