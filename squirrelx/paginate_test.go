package squirrelx

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Masterminds/squirrel"

	"github.com/svyatoslavratov/go-paginate"
)

func itemsConfig() paginate.Config {
	return paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "items.id", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "name", Column: "items.name", Sortable: true, Searchable: true, Filter: paginate.AllOperators},
			{Name: "createdAt", Column: "items.created_at", Type: paginate.Time, Sortable: true},
			{Name: "tags", Column: "items.tags", Filter: []paginate.Operator{paginate.OpContains}},
			{Name: "price", Column: "items.price", Type: paginate.Number, Filter: []paginate.Operator{paginate.OpBtw, paginate.OpGte}},
		},
		DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
		TieBreak:    "items.id",
	}
}

func TestStatements(t *testing.T) {
	t.Parallel()
	base := squirrel.Select("items.id", "items.name").From("items").Where(squirrel.Eq{"items.shop_id": 7})
	tests := []struct {
		name          string
		qb            squirrel.SelectBuilder
		req           paginate.Request
		config        func(cfg *paginate.Config)
		wantCount     string
		wantCountArgs []any
		wantEstimate  string
		wantPage      string
		wantPageArgs  []any
	}{
		{
			name:          "default page: default sort and tie-breaker",
			qb:            base,
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1) AS paginate_count",
			wantCountArgs: []any{7},
			wantPage:      "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 ORDER BY items.created_at DESC, items.id ASC LIMIT 20 OFFSET 0",
			wantPageArgs:  []any{7},
		},
		{
			name: "search, filters and sort",
			qb:   base,
			req: paginate.Request{
				Page:   3,
				Limit:  5,
				Search: "50%",
				SortBy: []paginate.Sort{{Field: "name", Order: paginate.Asc}},
				Filter: paginate.FilterQuery{
					"id":    {"$in:1,2"},
					"tags":  {"$contains:red,big"},
					"price": {"$btw:10,20"},
					"name":  {"$sw:a", "$or:$null"},
				},
			},
			wantCount: "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND CAST(items.name AS TEXT) ILIKE $2 AND " +
				"((items.id IN ($3, $4)) AND ((CAST(items.name AS TEXT) ILIKE $5) OR (items.name IS NULL)) AND (items.price >= $6 AND items.price <= $7) AND (items.tags @> $8))) AS paginate_count",
			wantCountArgs: []any{7, `%50\%%`, "1", "2", "a%", "10", "20", `{"red","big"}`},
			wantPage: "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND CAST(items.name AS TEXT) ILIKE $2 AND " +
				"((items.id IN ($3, $4)) AND ((CAST(items.name AS TEXT) ILIKE $5) OR (items.name IS NULL)) AND (items.price >= $6 AND items.price <= $7) AND (items.tags @> $8)) " +
				"ORDER BY items.name ASC, items.id ASC LIMIT 5 OFFSET 10",
			wantPageArgs: []any{7, `%50\%%`, "1", "2", "a%", "10", "20", `{"red","big"}`},
		},
		{
			name: "group by counts groups through the subquery",
			qb:   squirrel.Select("items.name", "COUNT(*) AS total").From("items").GroupBy("items.name"),
			config: func(cfg *paginate.Config) {
				cfg.TieBreak = ""
				cfg.DefaultSort = []paginate.Sort{{Field: "name", Order: paginate.Asc}}
			},
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.name, COUNT(*) AS total FROM items GROUP BY items.name) AS paginate_count",
			wantCountArgs: nil,
			wantPage:      "SELECT items.name, COUNT(*) AS total FROM items GROUP BY items.name ORDER BY items.name ASC LIMIT 20 OFFSET 0",
			wantPageArgs:  nil,
		},
		{
			name:         "every row when the config allows it: no count, no LIMIT",
			qb:           base,
			req:          paginate.Request{RequestedMode: paginate.ModeDisabled},
			config:       func(cfg *paginate.Config) { cfg.AllowDisabledPagination = true },
			wantPage:     "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 ORDER BY items.created_at DESC, items.id ASC",
			wantPageArgs: []any{7},
		},
		{
			name:          "an estimate: EXPLAIN of the matching rows, the count for small results",
			qb:            base,
			config:        func(cfg *paginate.Config) { cfg.Count = paginate.CountEstimate },
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1) AS paginate_count",
			wantCountArgs: []any{7},
			wantEstimate:  "EXPLAIN (FORMAT JSON) SELECT items.id, items.name FROM items WHERE items.shop_id = $1",
			wantPage:      "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 ORDER BY items.created_at DESC, items.id ASC LIMIT 20 OFFSET 0",
			wantPageArgs:  []any{7},
		},
		{
			name:         "no count: a row more than the page",
			qb:           base,
			req:          paginate.Request{Limit: 5},
			config:       func(cfg *paginate.Config) { cfg.Count = paginate.CountNone },
			wantPage:     "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 ORDER BY items.created_at DESC, items.id ASC LIMIT 6 OFFSET 0",
			wantPageArgs: []any{7},
		},
		{
			name:   "cursor pagination: the keyset instead of OFFSET, back from the cursor",
			qb:     base,
			req:    paginate.Request{Limit: 5, Cursor: backCursor()},
			config: func(cfg *paginate.Config) { cfg.Pagination = paginate.CursorPagination },
			wantPage: "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND ((items.created_at > $2 OR items.created_at IS NULL) OR ((items.created_at = $3) AND (items.id < $4))) " +
				"ORDER BY items.created_at ASC NULLS LAST, items.id DESC NULLS FIRST LIMIT 6 OFFSET 0",
			wantPageArgs: []any{7, "2024-05-01T10:00:00Z", "2024-05-01T10:00:00Z", "7"},
		},
		{
			name:          "compare as text",
			qb:            base,
			req:           paginate.Request{Filter: paginate.FilterQuery{"id": {"$in:1,2"}}},
			config:        func(cfg *paginate.Config) { cfg.CompareAsText = true },
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND CAST(items.id AS TEXT) IN ($2, $3)) AS paginate_count",
			wantCountArgs: []any{7, "1", "2"},
			wantPage:      "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND CAST(items.id AS TEXT) IN ($2, $3) ORDER BY items.created_at DESC, items.id ASC LIMIT 20 OFFSET 0",
			wantPageArgs:  []any{7, "1", "2"},
		},
		{
			name: "filter expression ANDed with filter.<field>",
			qb:   base,
			req: paginate.Request{
				Filter:           paginate.FilterQuery{"price": {"$gte:10"}},
				FilterExpression: `(id=$in:1,2 OR name=$sw:"big red") AND NOT tags=$contains:sale`,
			},
			wantCount: "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND items.price >= $2 AND " +
				"(((items.id IN ($3, $4)) OR (CAST(items.name AS TEXT) ILIKE $5)) AND (NOT (items.tags @> $6)))) AS paginate_count",
			wantCountArgs: []any{7, "10", "1", "2", "big red%", `{"sale"}`},
			wantPage: "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND items.price >= $2 AND " +
				"(((items.id IN ($3, $4)) OR (CAST(items.name AS TEXT) ILIKE $5)) AND (NOT (items.tags @> $6))) ORDER BY items.created_at DESC, items.id ASC LIMIT 20 OFFSET 0",
			wantPageArgs: []any{7, "10", "1", "2", "big red%", `{"sale"}`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := itemsConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			req := tc.req
			plan, err := Statements(tc.qb, &req, paginate.MustCompile(cfg))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertStatement(t, "count", plan.Count, tc.wantCount, tc.wantCountArgs)
			assertStatement(t, "estimate", plan.Estimate, tc.wantEstimate, tc.wantCountArgs)
			assertStatement(t, "page", plan.Page, tc.wantPage, tc.wantPageArgs)
		})
	}
}

func assertStatement(t *testing.T, name string, got Statement, wantSQL string, wantArgs []any) {
	t.Helper()
	if wantSQL == "" {
		if got.SQL != "" {
			t.Fatalf("want no %s statement, got %s", name, got.SQL)
		}
		return
	}
	if got.SQL != wantSQL {
		t.Fatalf("%s sql:\nwant %s\ngot  %s", name, wantSQL, got.SQL)
	}
	if len(got.Args) != 0 || len(wantArgs) != 0 {
		if !reflect.DeepEqual(got.Args, wantArgs) {
			t.Fatalf("%s args: want %#v, got %#v", name, wantArgs, got.Args)
		}
	}
}

func TestStatementsErrors(t *testing.T) {
	t.Parallel()
	base := squirrel.Select("items.id").From("items")
	tests := []struct {
		name   string
		req    paginate.Request
		config func(cfg *paginate.Config)
		code   paginate.Code
	}{
		{name: "operator not allowed", req: paginate.Request{Filter: paginate.FilterQuery{"id": {"$gt:1"}}}, code: paginate.CodeOperatorNotAllowed},
		{name: "unknown field", req: paginate.Request{Filter: paginate.FilterQuery{"secret": {"1"}}}, code: paginate.CodeUnknownField},
		{name: "value of the wrong type", req: paginate.Request{Filter: paginate.FilterQuery{"price": {"$gte:cheap"}}}, code: paginate.CodeInvalidValue},
		{name: "malformed expression", req: paginate.Request{FilterExpression: "(id=$in:1"}, code: paginate.CodeInvalidExpression},
		{
			name:   "expression terms are checked with Lenient too",
			req:    paginate.Request{FilterExpression: "secret=1 OR id=$in:1"},
			config: func(cfg *paginate.Config) { cfg.Lenient = true },
			code:   paginate.CodeUnknownField,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := itemsConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			req := tc.req
			_, err := Statements(base, &req, paginate.MustCompile(cfg))
			var pe *paginate.Error
			if !errors.As(err, &pe) || pe.Code != tc.code || !errors.Is(err, paginate.ErrInvalidRequest) {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
}

// backCursor is a cursor of itemsConfig in cursor pagination: back from the row created at
// 2024-05-01T10:00:00Z with id 7.
func backCursor() string {
	cfg := itemsConfig()
	cfg.Pagination = paginate.CursorPagination
	endpoint := paginate.MustCompile(cfg)
	q, err := endpoint.Prepare(&paginate.Request{Limit: 1, Cursor: forwardCursor(endpoint)})
	if err != nil {
		panic(err)
	}
	page, err := paginate.NewResponse(q, []map[string]any{{"id": 7, "createdAt": "2024-05-01T10:00:00Z"}}, paginate.Total{})
	if err != nil {
		panic(err)
	}
	return page.Meta.PreviousCursor
}

func forwardCursor(endpoint *paginate.Endpoint) string {
	q, err := endpoint.Prepare(&paginate.Request{Limit: 1})
	if err != nil {
		panic(err)
	}
	page, err := paginate.NewResponse(q, []map[string]any{{"id": 1, "createdAt": "2024-05-02T10:00:00Z"}, {"id": 2, "createdAt": "2024-05-01T12:00:00Z"}}, paginate.Total{})
	if err != nil {
		panic(err)
	}
	return page.Meta.NextCursor
}
