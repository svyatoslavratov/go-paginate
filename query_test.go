package paginate

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func usersConfig() Config {
	cfg := NewConfig("users", []string{"users.created_at", "users.name"}, map[string]string{
		"createdAt": "users.created_at",
		"name":      "users.name",
		"email":     "users.email",
		"fullName":  "users.name",
		"age":       "users.age",
	})
	cfg.SearchableColumns = []string{"users.name", "users.email"}
	cfg.FilterableColumns = FilterableColumns{"users.name": AllOperators, "users.email": {}, "users.age": {OpGte}}
	cfg.TieBreakColumn = "users.id"
	return *cfg
}

func TestNormalizeConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := Config{TableName: "users", DefaultLimit: 500}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxLimit != DefaultMaxLimit || cfg.DefaultLimit != DefaultMaxLimit {
		t.Fatalf("the default limit must be capped by the max limit, got %d/%d", cfg.DefaultLimit, cfg.MaxLimit)
	}
	if cfg.MaxSearchLength != DefaultMaxSearchLength || cfg.MaxFilterConditions != DefaultMaxFilterConditions ||
		cfg.MaxFilterValues != DefaultMaxFilterValues || cfg.MaxFilterExpressionComplexity != DefaultMaxFilterExpressionComplexity {
		t.Fatalf("caps must get defaults, got %+v", cfg)
	}
	if err := normalizeConfig(&Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want a config error without a table, got %v", err)
	}
}

func TestPrepare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		req     Request
		config  func(cfg *Config)
		check   func(t *testing.T, req Request, q *Query)
		wantErr string
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
		{name: "page whose offset overflows", req: Request{Page: math.MaxUint64, Limit: 100}, wantErr: "is too large"},
		{name: "largest page that fits", req: Request{Page: math.MaxInt/100 + 1, Limit: 100}},
		{
			name: "fields map to columns, order is normalized",
			req: Request{
				SortBy:   SortBy{"createdAt": "DESC"},
				SortKeys: []string{"createdAt"},
				Search:   "ann",
				SearchBy: []string{"email"},
				Filter:   FilterQuery{"name": {"$sw:a"}, "fullName": {"$or:b"}},
			},
			check: func(t *testing.T, _ Request, q *Query) {
				if !reflect.DeepEqual(q.Sort, []SortEntry{{"users.created_at", Desc}}) {
					t.Fatalf("sort: %v", q.Sort)
				}
				want := []Condition{
					{SQL: "CAST(users.email AS TEXT) ILIKE ?", Args: []any{"%ann%"}},
					// fullName and name map to one column: their values chain in field order, and the
					// $or of the first value of a chain is not used inside it.
					{SQL: "(users.name = ?) AND (CAST(users.name AS TEXT) ILIKE ?)", Args: []any{"b", "a%"}},
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
		{name: "order that is neither asc nor desc", req: Request{SortBy: SortBy{"createdAt": "desc; DROP TABLE users"}}, wantErr: "sort order of createdAt"},
		{name: "unknown sort field", req: Request{SortBy: SortBy{"password": Asc}}, wantErr: "sorting by password is not allowed"},
		{
			name:   "unknown fields are dropped without strict validation",
			req:    Request{SortBy: SortBy{"password": Asc}, SearchBy: []string{"password"}, Filter: FilterQuery{"password": {"x"}}},
			config: func(cfg *Config) { cfg.StrictValidate = false },
			check: func(t *testing.T, _ Request, q *Query) {
				if len(q.Where) != 0 || !reflect.DeepEqual(q.Sort, []SortEntry{{"users.created_at", Asc}}) {
					t.Fatalf("unknown fields must be dropped: %+v", q)
				}
			},
		},
		{name: "unknown searchBy field", req: Request{SearchBy: []string{"password"}}, wantErr: "searching by password is not allowed"},
		{
			name:   "ignored searchBy is not checked",
			req:    Request{Search: "x", SearchBy: []string{"password"}},
			config: func(cfg *Config) { cfg.IgnoreSearchByInQueryParam = true },
			check: func(t *testing.T, _ Request, q *Query) {
				if len(q.Where) != 1 || !strings.Contains(q.Where[0].SQL, "users.email") {
					t.Fatalf("the search must cover every searchable column: %+v", q.Where)
				}
			},
		},
		{name: "unknown filter field", req: Request{Filter: FilterQuery{"password": {"x"}}}, wantErr: "unknown filter field password"},
		{
			name:    "malformed expression fails even without strict validation",
			req:     Request{FilterExpression: "(name=a"},
			config:  func(cfg *Config) { cfg.StrictValidate = false },
			wantErr: `expected ")"`,
		},
		{name: "search over the cap", req: Request{Search: strings.Repeat("я", 11)}, config: func(cfg *Config) { cfg.MaxSearchLength = 10 }, wantErr: "longer than 10 characters"},
		{name: "search at the cap counts characters, not bytes", req: Request{Search: strings.Repeat("я", 10)}, config: func(cfg *Config) { cfg.MaxSearchLength = 10 }},
		{
			name:    "too many filter values, unknown fields included",
			req:     Request{Filter: FilterQuery{"name": {"a", "b"}, "password": {"c"}}},
			config:  func(cfg *Config) { cfg.StrictValidate = false; cfg.MaxFilterConditions = 2 },
			wantErr: "too many filter values",
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
			name:   "client asks for every row: allowed by the config",
			req:    clientMode(ModeDisabled),
			config: func(cfg *Config) { cfg.AllowDisabledPagination = true },
			check: func(t *testing.T, req Request, q *Query) {
				if q.Mode != ModeDisabled || req.Mode != ModeDisabled {
					t.Fatalf("mode %q/%q", q.Mode, req.Mode)
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
			cfg := usersConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			req := tc.req
			q, err := Prepare(&req, cfg)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("want an invalid request with %q, got %v", tc.wantErr, err)
				}
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
	q := Query{filtered: true}
	if err := q.WrapError("select", dataException); !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "invalid input syntax") {
		t.Fatalf("want an invalid request, got %v", err)
	}
	if err := (&Query{}).WrapError("select", dataException); errors.Is(err, ErrInvalidRequest) {
		t.Fatal("without filters a data exception is the server's problem")
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

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	if err := usersConfig().Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	broken := usersConfig()
	broken.TableName = ""
	broken.SortableColumns = append(broken.SortableColumns, "rating")
	broken.SearchableColumns = append(broken.SearchableColumns, "users.bio")
	broken.FilterableColumns["users.phone"] = []Operator{"$regex"}
	broken.DefaultSortBy = SortBy{"users.name": "up"}
	broken.DefaultLimit, broken.MaxLimit = 50, 10
	broken.Format = "xml"
	err := broken.Validate()
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want a config error, got %v", err)
	}
	for _, want := range []string{
		"tableName is required",
		"sortable column rating: no API field maps to it",
		"searchable column users.bio: no API field maps to it",
		"filterable column users.phone: no API field maps to it",
		"filterable column users.phone: unknown operator $regex",
		`default sort users.name: order must be asc or desc, got "up"`,
		"defaultLimit 50 exceeds maxLimit 10",
		`unknown format "xml"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}
