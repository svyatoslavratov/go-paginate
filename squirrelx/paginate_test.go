package squirrelx

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Masterminds/squirrel"

	"github.com/svyatoslavratov/go-paginate"
)

func itemsConfig() paginate.Config {
	cfg := paginate.NewConfig("items", []string{"items.created_at", "items.name"}, map[string]string{
		"id":        "items.id",
		"name":      "items.name",
		"createdAt": "items.created_at",
		"tags":      "items.tags",
		"price":     "items.price",
	})
	cfg.SearchableColumns = []string{"items.name"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"items.id":    {paginate.OpIn},
		"items.name":  paginate.AllOperators,
		"items.tags":  {paginate.OpContains},
		"items.price": {paginate.OpBtw, paginate.OpGte},
	}
	cfg.DefaultSortBy = paginate.SortBy{"items.created_at": paginate.Desc}
	cfg.TieBreakColumn = "items.id"
	return *cfg
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
		wantPage      string
		wantPageArgs  []any
	}{
		{
			name:          "default page: default sort and tie-breaker",
			qb:            base,
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1) AS paginate_count",
			wantCountArgs: []any{7},
			wantPage:      "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 ORDER BY items.created_at desc, items.id asc LIMIT 20 OFFSET 0",
			wantPageArgs:  []any{7},
		},
		{
			name: "search, filters and sort",
			qb:   base,
			req: paginate.Request{
				Page:     3,
				Limit:    5,
				Search:   "50%",
				SortBy:   paginate.SortBy{"name": paginate.Asc},
				SortKeys: []string{"name"},
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
				"ORDER BY items.name asc, items.id asc LIMIT 5 OFFSET 10",
			wantPageArgs: []any{7, `%50\%%`, "1", "2", "a%", "10", "20", `{"red","big"}`},
		},
		{
			name: "group by counts groups through the subquery",
			qb:   squirrel.Select("items.name", "COUNT(*) AS total").From("items").GroupBy("items.name"),
			config: func(cfg *paginate.Config) {
				cfg.TieBreakColumn = ""
				cfg.DefaultSortBy = paginate.SortBy{"items.name": paginate.Asc}
			},
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.name, COUNT(*) AS total FROM items GROUP BY items.name) AS paginate_count",
			wantCountArgs: nil,
			wantPage:      "SELECT items.name, COUNT(*) AS total FROM items GROUP BY items.name ORDER BY items.name asc LIMIT 20 OFFSET 0",
			wantPageArgs:  nil,
		},
		{
			name:          "every row when the config allows it",
			qb:            base,
			req:           paginate.Request{RequestedMode: paginate.ModeDisabled},
			config:        func(cfg *paginate.Config) { cfg.AllowDisabledPagination = true },
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1) AS paginate_count",
			wantCountArgs: []any{7},
			wantPage:      "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 ORDER BY items.created_at desc, items.id asc",
			wantPageArgs:  []any{7},
		},
		{
			name:          "compare as text",
			qb:            base,
			req:           paginate.Request{Filter: paginate.FilterQuery{"id": {"$in:1,2"}}},
			config:        func(cfg *paginate.Config) { cfg.CompareAsText = true },
			wantCount:     "SELECT COUNT(*) FROM (SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND CAST(items.id AS TEXT) IN ($2, $3)) AS paginate_count",
			wantCountArgs: []any{7, "1", "2"},
			wantPage:      "SELECT items.id, items.name FROM items WHERE items.shop_id = $1 AND CAST(items.id AS TEXT) IN ($2, $3) ORDER BY items.created_at desc, items.id asc LIMIT 20 OFFSET 0",
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
				"(((items.id IN ($3, $4)) OR (CAST(items.name AS TEXT) ILIKE $5)) AND (NOT (items.tags @> $6))) ORDER BY items.created_at desc, items.id asc LIMIT 20 OFFSET 0",
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
			count, page, err := Statements(tc.qb, &req, cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertStatement(t, "count", count, tc.wantCount, tc.wantCountArgs)
			assertStatement(t, "page", page, tc.wantPage, tc.wantPageArgs)
		})
	}
}

func assertStatement(t *testing.T, name string, got Statement, wantSQL string, wantArgs []any) {
	t.Helper()
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
		want   error
	}{
		{name: "no table", config: func(cfg *paginate.Config) { cfg.TableName = "" }, want: paginate.ErrInvalidConfig},
		{name: "no sortable columns", config: func(cfg *paginate.Config) { cfg.SortableColumns = nil }, want: paginate.ErrInvalidConfig},
		{name: "operator not allowed", req: paginate.Request{Filter: paginate.FilterQuery{"id": {"$gt:1"}}}, want: paginate.ErrInvalidRequest},
		{name: "unknown field", req: paginate.Request{Filter: paginate.FilterQuery{"secret": {"1"}}}, want: paginate.ErrInvalidRequest},
		{name: "malformed expression", req: paginate.Request{FilterExpression: "(id=$in:1"}, want: paginate.ErrInvalidRequest},
		{
			name:   "expression terms are validated without StrictValidate",
			req:    paginate.Request{FilterExpression: "secret=1 OR id=$in:1"},
			config: func(cfg *paginate.Config) { cfg.StrictValidate = false },
			want:   paginate.ErrInvalidRequest,
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
			if _, _, err := Statements(base, &req, cfg); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}
