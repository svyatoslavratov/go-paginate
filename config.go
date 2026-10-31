package paginate

// Field is an API field of an endpoint: the name clients use, the SQL column behind it, its type,
// and what clients may do with it.
//
//	paginate.Field{
//		Name:     "createdAt",
//		Column:   "users.created_at",
//		Type:     paginate.Time,
//		Sortable: true,
//		Filter:   []paginate.Operator{paginate.OpGte, paginate.OpLt, paginate.OpBtw},
//	}
//
// A field that allows nothing is still useful: the default sort and cursors may use it.
type Field struct {
	// Name is the API name of sortBy, searchBy and filter: "createdAt", "author.name".
	Name string
	// Column is the SQL the field reads: a column as the query names it ("users.created_at",
	// "u.name") or an expression ("COALESCE(u.nickname, u.name)").
	Column string
	// Type checks filter values and cursors before the query runs. The zero Type leaves the check
	// to the database.
	Type Type
	// Sortable lets clients sort by the field.
	Sortable bool
	// Searchable makes the field part of ?search=.
	Searchable bool
	// Filter lists the operators clients may filter the field with; nil makes it not filterable.
	// $eq is allowed on every filterable field, so []Operator{OpEq} allows $eq only and
	// AllOperators every operator. $not goes with any allowed operator.
	Filter []Operator
	// NotNull tells that the column has no NULL values. Cursor pagination then needs no NULL checks
	// for it, so Postgres can start an index scan right at the cursor. Wrong, it skips the rows
	// with NULL.
	NotNull bool
}

// Pagination is how an endpoint pages: by page number or by cursor.
type Pagination int

const (
	// OffsetPagination pages by number: ?page=3 skips two pages with OFFSET. Clients can open any
	// page and see the total, but deep pages get slower, and rows inserted or deleted meanwhile shift
	// the pages.
	OffsetPagination Pagination = iota
	// CursorPagination pages by cursor, also known as keyset pagination: ?cursor= points at the last
	// row the client saw, and the next page is the rows after it in the sort order, found with WHERE
	// instead of OFFSET. With an index on the sort keys, a deep page costs what the first one does
	// when the main key needs no NULL checks (see Field.NotNull), and no row is skipped or repeated
	// when rows change; but clients move only to the next or the previous page, and there is no
	// total. It needs a TieBreak, and the rows the executor reads must carry the sort fields.
	CursorPagination
)

// Count is how an endpoint counts the rows that match a request, for totalItems and totalPages.
type Count int

const (
	// CountExact runs SELECT COUNT(*): exact, but it reads every matching row.
	CountExact Count = iota
	// CountEstimate takes the planner's estimate from EXPLAIN: one cheap query whatever the table
	// size, as close as the table statistics are, and usually rougher with filters. With
	// ExactCountBelow, small results are counted exactly. The answer marks the total as estimated.
	CountEstimate
	// CountNone counts nothing: the executor reads one row more than the page to tell whether a next
	// page exists. The answer has no totalItems and totalPages, and links to no last page.
	CountNone
)

// String names the count: "exact", "estimate" or "none".
func (c Count) String() string {
	switch c {
	case CountEstimate:
		return "estimate"
	case CountNone:
		return "none"
	}
	return "exact"
}

// Config is the server side of a paginated endpoint: its fields, how it pages and counts, and its
// limits. Compile checks it and turns it into an Endpoint, once, at startup. Zero limits take the
// Default* constants.
type Config struct {
	// Fields are the API fields of the endpoint. Clients reach nothing else.
	Fields []Field

	// DefaultSort is the sort of a request without sortBy, in API names; the first key is the
	// main one. Any field may be a key here, sortable or not. Empty: the first sortable field
	// ascending, or only the TieBreak.
	DefaultSort []Sort

	// TieBreak is a column with unique values (usually the primary key, "users.id") that ends every
	// ORDER BY, so rows with equal sort values keep their order from page to page. Cursor
	// pagination requires it. Empty: no tie-breaker.
	TieBreak string

	// NullSort puts NULL values first or last in every sort key; zero keeps the database default
	// (NULLS LAST for ASC, NULLS FIRST for DESC in Postgres).
	NullSort NullSort

	// Pagination is OffsetPagination (page numbers, the default) or CursorPagination.
	Pagination Pagination

	// Count is how pages are counted: CountExact (the default), CountEstimate or CountNone. Cursor
	// pagination counts nothing. A count-only request (?limit=0) is counted exactly under
	// CountNone: counting is all it asks for.
	Count Count

	// ExactCountBelow replaces an estimate below it with an exact count, under CountEstimate: big
	// results get the cheap estimate, small ones an exact number. Zero: always the estimate.
	ExactCountBelow uint64

	// ParallelCount runs the count and the page at the same time, on two connections. The bundled
	// executors run them one after the other inside a transaction, which has one connection.
	ParallelCount bool

	// MultiWordSearch splits the search term on whitespace: every word must match some searchable
	// field ("big red" finds "red big box"). Off: the term is matched as a whole.
	MultiWordSearch bool

	// IgnoreSearchByInQueryParam ignores ?searchBy=: the search always covers every searchable
	// field.
	IgnoreSearchByInQueryParam bool

	// CompareAsText makes $eq and $in compare CAST(column AS TEXT). Off (the default): values are
	// compared with the column's own type, which uses indexes and matches numbers and dates by
	// value.
	CompareAsText bool

	// Lenient drops unknown fields and operators that are not allowed from a request instead of
	// answering 400, the way nestjs-paginate does. Malformed values, values over the caps, filter
	// expressions and cursors fail either way.
	Lenient bool

	// AllowDisabledPagination lets clients ask for every row at once with ?limit=-1 or
	// ?paginationMode=disabled. Off: such requests get the first page. A mode server code sets in
	// Request.Mode is applied regardless.
	AllowDisabledPagination bool

	// AllowWithDeletedInQuery lets clients ask for soft-deleted rows with ?withDeleted=true. The
	// GORM executor runs such requests Unscoped.
	AllowWithDeletedInQuery bool

	// DefaultLimit is the page size of a request without limit. Default DefaultLimit.
	DefaultLimit uint64

	// MaxLimit caps the page size a client may ask for. Default DefaultMaxLimit.
	MaxLimit uint64

	// MaxSearchLength caps the search term, in characters. Default DefaultMaxSearchLength.
	MaxSearchLength uint64

	// MaxFilterConditions caps the number of filter.<field> values in one request.
	// Default DefaultMaxFilterConditions.
	MaxFilterConditions uint64

	// MaxFilterValues caps the values of one $in, $contains or $btw list.
	// Default DefaultMaxFilterValues.
	MaxFilterValues uint64

	// MaxFilterExpressionComplexity caps the nodes of a filter=<expression>: terms, AND, OR, NOT and
	// parenthesized groups. Default DefaultMaxFilterExpressionComplexity.
	MaxFilterExpressionComplexity uint64

	// Format is the JSON shape of the answer: this package's (FormatDefault) or nestjs-paginate's
	// (FormatNestJS), with links to the neighboring pages.
	Format Format

	// LinksBaseURL prefixes the links of the answer: empty gives relative links
	// ("/users?page=2&limit=20"), "https://api.example.com" absolute ones, "/api" restores a prefix
	// a proxy strips. The request's Host header is never used, so a client cannot put its own host
	// into the links.
	LinksBaseURL string
}

// Field returns the field named name, to change it in code. Fields read from tags by ConfigOf get
// that way what tags cannot say, such as enum values from Go constants:
//
//	cfg, err := paginate.ConfigOf[User]("users")
//	cfg.Field("status").Type = paginate.Enum(domain.UserStatuses...)
//
// It returns nil when there is no such field. The pointer points into Fields, and appending to
// Fields may move them: take it after the appends.
func (c *Config) Field(name string) *Field {
	for i := range c.Fields {
		if c.Fields[i].Name == name {
			return &c.Fields[i]
		}
	}
	return nil
}
