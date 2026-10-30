package paginate

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func usersConfig() Config {
	return Config{
		Fields: []Field{
			{Name: "id", Column: "users.id", Type: Int},
			{Name: "createdAt", Column: "users.created_at", Type: Time, Sortable: true},
			{Name: "name", Column: "users.name", Sortable: true, Searchable: true, Filter: AllOperators},
			{Name: "email", Column: "users.email", Searchable: true, Filter: []Operator{OpEq}},
			{Name: "fullName", Column: "users.name", Filter: []Operator{OpEq}},
			{Name: "age", Column: "users.age", Type: Int, Filter: []Operator{OpGte}},
		},
		TieBreak: "users.id",
	}
}

// compile compiles usersConfig changed by change.
func compile(t testing.TB, change func(cfg *Config)) *Endpoint {
	t.Helper()
	cfg := usersConfig()
	if change != nil {
		change(&cfg)
	}
	e, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// wantError checks that err is an invalid request with code, field and message.
func wantError(t *testing.T, err error, code Code, field, message string) {
	t.Helper()
	var pe *Error
	if !errors.As(err, &pe) || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want an invalid request, got %v", err)
	}
	if pe.Code != code || pe.Field != field || !strings.Contains(pe.Message, message) {
		t.Fatalf("want %s/%q/%q, got %s/%q/%q", code, field, message, pe.Code, pe.Field, pe.Message)
	}
}

func TestPrepare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		req     Request
		config  func(cfg *Config)
		check   func(t *testing.T, req Request, q *Query)
		code    Code
		field   string
		message string
	}{
		{
			name: "defaults",
			check: func(t *testing.T, req Request, q *Query) {
				if req.Page != 1 || req.Limit != DefaultLimit || req.Mode != ModeDefault || q.Offset != 0 || q.Limit != DefaultLimit {
					t.Fatalf("req %+v, query %+v", req, q)
				}
				if !reflect.DeepEqual(q.Sort, []SortEntry{{"users.created_at", Asc}}) || q.TieBreak != "users.id" {
					t.Fatalf("sort: %v %q", q.Sort, q.TieBreak)
				}
				if q.Count != CountExact || q.FetchLimit() != DefaultLimit || q.Keyset {
					t.Fatalf("count %v, fetch %d, keyset %v", q.Count, q.FetchLimit(), q.Keyset)
				}
			},
		},
		{
			name: "limit is capped, offset follows the page",
			req:  Request{Page: 3, Limit: 1000},
			check: func(t *testing.T, req Request, q *Query) {
				if req.Limit != DefaultMaxLimit || q.Offset != 2*DefaultMaxLimit {
					t.Fatalf("limit %d, offset %d", req.Limit, q.Offset)
				}
			},
		},
		{name: "page whose offset overflows", req: Request{Page: math.MaxUint64, Limit: 100}, code: CodeInvalidPage, message: "is too large"},
		{name: "largest page that fits", req: Request{Page: math.MaxInt/100 + 1, Limit: 100}},
		{
			name: "fields map to columns, the order is normalized",
			req: Request{
				SortBy:   []Sort{{Field: "createdAt", Order: "desc"}},
				Search:   "ann",
				SearchBy: []string{"email"},
				Filter:   FilterQuery{"name": {"$sw:a"}, "fullName": {"$or:b"}},
			},
			check: func(t *testing.T, req Request, q *Query) {
				if !reflect.DeepEqual(q.Sort, []SortEntry{{"users.created_at", Desc}}) || req.SortBy[0].Order != Desc {
					t.Fatalf("sort: %v, request %v", q.Sort, req.SortBy)
				}
				want := []Condition{
					{SQL: "CAST(users.email AS TEXT) ILIKE ?", Args: []any{"%ann%"}},
					// fullName starts with $or, so it opens the OR group; name is ANDed with it.
					{SQL: "((users.name = ?) AND (CAST(users.name AS TEXT) ILIKE ?))", Args: []any{"b", "a%"}},
				}
				if !reflect.DeepEqual(q.Where, want) {
					t.Fatalf("where:\nwant %#v\ngot  %#v", want, q.Where)
				}
			},
		},
		{
			name: "filter.<field> and an expression are ANDed",
			req:  Request{Filter: FilterQuery{"age": {"$gte:18"}}, FilterExpression: "name=$eq:a OR email=b"},
			check: func(t *testing.T, _ Request, q *Query) {
				want := []Condition{
					{SQL: "users.age >= ?", Args: []any{"18"}},
					{SQL: "((users.name = ?) OR (users.email = ?))", Args: []any{"a", "b"}},
				}
				if !reflect.DeepEqual(q.Where, want) {
					t.Fatalf("where:\nwant %#v\ngot  %#v", want, q.Where)
				}
			},
		},
		{
			name:    "order that is neither ASC nor DESC",
			req:     Request{SortBy: []Sort{{Field: "createdAt", Order: "desc; DROP TABLE users"}}},
			code:    CodeInvalidSort,
			field:   "createdAt",
			message: "sort order of createdAt",
		},
		{name: "unknown sort field", req: Request{SortBy: []Sort{{"password", Asc}}}, code: CodeSortNotAllowed, field: "password", message: "sorting by password is not allowed"},
		{name: "field that is not sortable", req: Request{SortBy: []Sort{{"email", Asc}}}, code: CodeSortNotAllowed, field: "email", message: "sorting by email"},
		{
			name:   "Lenient drops unknown fields and what is not allowed",
			req:    Request{SortBy: []Sort{{"password", Asc}, {"email", Asc}}, SearchBy: []string{"password", "age"}, Filter: FilterQuery{"password": {"x"}, "createdAt": {"x"}, "email": {"$gt:a"}}},
			config: func(cfg *Config) { cfg.Lenient = true },
			check: func(t *testing.T, _ Request, q *Query) {
				if len(q.Where) != 0 || !reflect.DeepEqual(q.Sort, []SortEntry{{"users.created_at", Asc}}) {
					t.Fatalf("the request must be dropped: %+v", q)
				}
			},
		},
		{
			name:    "Lenient does not forgive invalid values",
			req:     Request{Filter: FilterQuery{"age": {"$gte:old"}}},
			config:  func(cfg *Config) { cfg.Lenient = true },
			code:    CodeInvalidValue,
			field:   "age",
			message: `invalid value "old" for age: must be an integer`,
		},
		{name: "unknown searchBy field", req: Request{SearchBy: []string{"password"}}, code: CodeSearchNotAllowed, field: "password", message: "searching by password is not allowed"},
		{name: "searchBy field that is not searchable", req: Request{SearchBy: []string{"age"}}, code: CodeSearchNotAllowed, field: "age", message: "searching by age"},
		{
			name:   "ignored searchBy is not checked",
			req:    Request{Search: "x", SearchBy: []string{"password"}},
			config: func(cfg *Config) { cfg.IgnoreSearchByInQueryParam = true },
			check: func(t *testing.T, _ Request, q *Query) {
				if len(q.Where) != 1 || !strings.Contains(q.Where[0].SQL, "users.email") {
					t.Fatalf("the search must cover every searchable field: %+v", q.Where)
				}
			},
		},
		{name: "unknown filter field", req: Request{Filter: FilterQuery{"password": {"x"}}}, code: CodeUnknownField, field: "password", message: "unknown filter field password"},
		{name: "field that is not filterable", req: Request{Filter: FilterQuery{"createdAt": {"x"}}}, code: CodeFilterNotAllowed, field: "createdAt", message: "filter on createdAt is not allowed"},
		{name: "operator that is not allowed", req: Request{Filter: FilterQuery{"email": {"$gt:a"}}}, code: CodeOperatorNotAllowed, field: "email", message: "operator $gt is not allowed for email"},
		{name: "value of the wrong type", req: Request{Filter: FilterQuery{"age": {"$gte:abc"}}}, code: CodeInvalidValue, field: "age", message: "must be an integer"},
		{
			name:    "malformed expression fails even with Lenient",
			req:     Request{FilterExpression: "(name=a"},
			config:  func(cfg *Config) { cfg.Lenient = true },
			code:    CodeInvalidExpression,
			message: `expected ")"`,
		},
		{
			name:    "search over the cap",
			req:     Request{Search: strings.Repeat("я", 11)},
			config:  func(cfg *Config) { cfg.MaxSearchLength = 10 },
			code:    CodeSearchTooLong,
			message: "longer than 10 characters",
		},
		{name: "search at the cap counts characters, not bytes", req: Request{Search: strings.Repeat("я", 10)}, config: func(cfg *Config) { cfg.MaxSearchLength = 10 }},
		{
			name:    "too many filter values, unknown fields included",
			req:     Request{Filter: FilterQuery{"name": {"a", "b"}, "password": {"c"}}},
			config:  func(cfg *Config) { cfg.Lenient = true; cfg.MaxFilterConditions = 2 },
			code:    CodeTooManyValues,
			message: "too many filter values",
		},
		{
			name: "client asks for every row: refused by default",
			req:  clientMode(ModeDisabled),
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Mode != ModeDefault {
					t.Fatalf("mode %q", q.Mode)
				}
			},
		},
		{
			name:   "client asks for every row: allowed by the config, read without a count",
			req:    clientMode(ModeDisabled),
			config: func(cfg *Config) { cfg.AllowDisabledPagination = true },
			check: func(t *testing.T, req Request, q *Query) {
				if q.Mode != ModeDisabled || req.Mode != ModeDisabled || q.Count != CountNone {
					t.Fatalf("mode %q/%q, count %v", q.Mode, req.Mode, q.Count)
				}
			},
		},
		{
			name: "server disables pagination itself",
			req:  Request{Mode: ModeDisabled},
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Mode != ModeDisabled {
					t.Fatal("a mode set by the server must stay")
				}
			},
		},
		{
			name: "server forces a page over an allowed client request",
			req: func() Request {
				req := clientMode(ModeDisabled)
				req.Mode = ModeDefault
				return req
			}(),
			config: func(cfg *Config) { cfg.AllowDisabledPagination = true },
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Mode != ModeDefault {
					t.Fatalf("mode %q", q.Mode)
				}
			},
		},
		{
			name: "count needs no permission",
			req:  clientMode(ModeCount),
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Mode != ModeCount {
					t.Fatalf("mode %q", q.Mode)
				}
			},
		},
		{
			name:   "CountNone reads a row ahead",
			config: func(cfg *Config) { cfg.Count = CountNone },
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Count != CountNone || q.FetchLimit() != DefaultLimit+1 {
					t.Fatalf("count %v, fetch %d", q.Count, q.FetchLimit())
				}
			},
		},
		{
			name:   "a count-only request is counted under CountNone",
			req:    clientMode(ModeCount),
			config: func(cfg *Config) { cfg.Count = CountNone },
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Count != CountExact {
					t.Fatalf("count %v", q.Count)
				}
			},
		},
		{
			name:   "estimates and the parallel count reach the query",
			config: func(cfg *Config) { cfg.Count = CountEstimate; cfg.ExactCountBelow = 1000; cfg.ParallelCount = true },
			check: func(t *testing.T, _ Request, q *Query) {
				if q.Count != CountEstimate || q.ExactCountBelow != 1000 || !q.Parallel || q.FetchLimit() != DefaultLimit {
					t.Fatalf("query %+v", q)
				}
			},
		},
		{
			name: "withDeleted needs the config",
			req:  Request{WithDeleted: true},
			check: func(t *testing.T, _ Request, q *Query) {
				if q.WithDeleted {
					t.Fatal("withDeleted without AllowWithDeletedInQuery")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := compile(t, tc.config)
			req := tc.req
			q, err := e.Prepare(&req)
			if tc.code != "" {
				wantError(t, err, tc.code, tc.field, tc.message)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, req, q)
			}
		})
	}
}

func clientMode(mode Mode) Request {
	var req Request
	req.requestMode(mode)
	return req
}

func TestWrapError(t *testing.T) {
	t.Parallel()
	dataException := stateError{state: "22P02", message: `invalid input syntax for type uuid: "x"`}
	q := Query{clientValues: true}
	err := q.WrapError("select", dataException)
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != CodeInvalidValue || !strings.Contains(err.Error(), "invalid input syntax") {
		t.Fatalf("want an invalid value, got %v", err)
	}
	if !errors.As(err, new(stateError)) {
		t.Fatal("the database error must stay reachable for logs")
	}
	if err := (&Query{}).WrapError("select", dataException); errors.Is(err, ErrInvalidRequest) {
		t.Fatal("without client values a data exception is the server's problem")
	}
	other := stateError{state: "42P01", message: "relation does not exist"}
	if err := q.WrapError("select", other); errors.Is(err, ErrInvalidRequest) || !errors.As(err, new(stateError)) {
		t.Fatalf("want the database error wrapped, got %v", err)
	}
}

type stateError struct {
	state, message string
}

func (e stateError) Error() string    { return e.message }
func (e stateError) SQLState() string { return e.state }

func TestCompile(t *testing.T) {
	t.Parallel()
	e := compile(t, func(cfg *Config) { cfg.DefaultLimit = 500 })
	cfg := e.Config()
	if cfg.MaxLimit != DefaultMaxLimit || cfg.DefaultLimit != DefaultMaxLimit {
		t.Fatalf("the default limit must be capped by the max limit, got %d/%d", cfg.DefaultLimit, cfg.MaxLimit)
	}
	if cfg.MaxSearchLength != DefaultMaxSearchLength || cfg.MaxFilterConditions != DefaultMaxFilterConditions ||
		cfg.MaxFilterValues != DefaultMaxFilterValues || cfg.MaxFilterExpressionComplexity != DefaultMaxFilterExpressionComplexity {
		t.Fatalf("caps must get defaults, got %+v", cfg)
	}

	broken := usersConfig()
	broken.Fields = append(broken.Fields,
		Field{Column: "users.bio"},
		Field{Name: "phone"},
		Field{Name: "name", Column: "users.nickname"},
		Field{Name: "rank", Column: "users.rank", Filter: []Operator{"$regex", OpOr}},
		Field{Name: "score", Column: "users.score", Type: Int, Filter: []Operator{OpContains}},
		Field{Name: "state", Column: "users.state", Type: Enum()},
	)
	broken.DefaultSort = []Sort{{"rating", Asc}, {"name", "up"}}
	broken.DefaultLimit, broken.MaxLimit = 50, 10
	broken.Format = "xml"
	broken.Pagination = CursorPagination
	broken.TieBreak = ""
	broken.Count = 7
	_, err := Compile(broken)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want a config error, got %v", err)
	}
	for _, want := range []string{
		"field 6: name is required",
		"field phone: column is required",
		"field name: defined twice",
		"field rank: unknown operator $regex",
		"field rank: $or is not an operator",
		"field score: $contains needs an array type, the field is int",
		"field state: an enum needs values",
		"default sort: unknown field rating",
		`default sort name: order must be ASC or DESC, got "up"`,
		"defaultLimit 50 exceeds maxLimit 10",
		`unknown format "xml"`,
		"cursor pagination needs a TieBreak",
		"unknown count 7",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}

	if _, err := Compile(Config{Fields: []Field{{Name: "a", Column: "t.a"}}}); err == nil || !strings.Contains(err.Error(), "no sort") {
		t.Fatalf("want an error for an endpoint without any sort, got %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustCompile must panic on a broken config")
		}
	}()
	MustCompile(broken)
}

func TestEndpointFieldsAreCopies(t *testing.T) {
	t.Parallel()
	e := compile(t, nil)
	fields := e.Fields()
	fields[2].Filter[0] = "$changed"
	fields[2].Name = "changed"
	if again := e.Fields(); again[2].Name != "name" || again[2].Filter[0] != OpEq {
		t.Fatalf("Fields must return copies, got %+v", again[2])
	}
}

func TestPrepareLeavesTheSortSliceAlone(t *testing.T) {
	t.Parallel()
	e := compile(t, nil)
	shared := []Sort{{Field: "createdAt", Order: "desc"}}
	req := Request{SortBy: shared}
	if _, err := e.Prepare(&req); err != nil {
		t.Fatal(err)
	}
	if shared[0].Order != "desc" || req.SortBy[0].Order != Desc {
		t.Fatalf("Prepare must normalize a copy: shared %v, request %v", shared, req.SortBy)
	}
}

func TestHugeNumbers(t *testing.T) {
	t.Parallel()
	req, err := ParseRequest(httptest.NewRequest(http.MethodGet, "/users?page=99999999999999999999", nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = compile(t, nil).Prepare(req)
	wantError(t, err, CodeInvalidPage, "", "is too large")

	req, err = ParseRequest(httptest.NewRequest(http.MethodGet, "/users?limit=99999999999999999999", nil))
	if err != nil {
		t.Fatal(err)
	}
	if q, err := compile(t, nil).Prepare(req); err != nil || q.Limit != DefaultMaxLimit {
		t.Fatalf("a huge limit is capped: %v %v", q, err)
	}
}

func TestFieldsListEq(t *testing.T) {
	t.Parallel()
	fields := compile(t, nil).Fields()
	if got := fields[5].Filter; !reflect.DeepEqual(got, []Operator{OpEq, OpGte}) {
		t.Fatalf("the operators of a filterable field include $eq: %v", got)
	}
	if fields[0].Filter != nil {
		t.Fatalf("a field that is not filterable has no operators: %v", fields[0].Filter)
	}
}
