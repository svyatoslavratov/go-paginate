package paginate

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// The fuzz tests feed the parsers arbitrary client input. Besides "no panic" they check the
// property that matters for safety: rendered SQL never contains client input. Every value is an
// argument, so the "?" placeholders match the arguments one to one, and the SQL text has no quotes,
// semicolons or comments.
//
// Run one target for a while with: go test -run='^$' -fuzz=FuzzParseQuery -fuzztime=1m .

func fuzzConfig() Config {
	return Config{
		Fields: []Field{
			{Name: "id", Column: "t.id", Type: Int, Sortable: true, Filter: AllOperators},
			{Name: "name", Column: "t.name", Sortable: true, Searchable: true, Filter: AllOperators},
			{Name: "price", Column: "t.price", Type: Number, Sortable: true, Filter: AllOperators},
			{Name: "tags", Column: "t.tags", Type: ArrayOf(Text), Filter: []Operator{OpContains, OpNull}},
			{Name: "createdAt", Column: "t.created_at", Type: Time, Sortable: true, Filter: AllOperators},
			{Name: "author", Column: "a.name", Searchable: true, Filter: []Operator{OpEq}},
		},
		TieBreak:                "t.id",
		MultiWordSearch:         true,
		AllowDisabledPagination: true,
		AllowWithDeletedInQuery: true,
		Format:                  FormatNestJS,
	}
}

func fuzzEndpoints() (offset, cursor *Endpoint) {
	cfg := fuzzConfig()
	offset = MustCompile(cfg)
	cfg.Pagination = CursorPagination
	cfg.NullSort = NullsLast
	return offset, MustCompile(cfg)
}

// checkSQL fails when a condition could carry client input in its text.
func checkSQL(t *testing.T, sql string, args []any) {
	t.Helper()
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Fatalf("%d placeholders for %d arguments in %q", got, len(args), sql)
	}
	if strings.ContainsAny(sql, "';\"") || strings.Contains(sql, "--") || strings.Contains(sql, "/*") {
		t.Fatalf("suspicious SQL text %q", sql)
	}
}

func checkError(t *testing.T, err error) {
	t.Helper()
	var pe *Error
	if !errors.Is(err, ErrInvalidRequest) || !errors.As(err, &pe) || pe.Code == "" {
		t.Fatalf("client input must fail with a coded ErrInvalidRequest, got %v", err)
	}
}

func FuzzParseFilterExpression(f *testing.F) {
	for _, seed := range []string{
		`name=$eq:Milo`,
		`(name=$eq:a OR price=$gte:3) AND NOT tags=$contains:x,y`,
		`name=$eq:"Milo \"the cat\""`,
		`name=$eq:'it\'s' OR name=$eq:"C:\Users"`,
		`((((id=1))))`,
		`NOT NOT NOT id=$null`,
		`id=1 AND`,
		`"unterminated`,
		`name=$or:x`,
		`price=$btw:1,2,3`,
		`createdAt=$lt:2024-01-01 OR author=x`,
		`a=1 OR`,
		`()`,
	} {
		f.Add(seed)
	}
	e, _ := fuzzEndpoints()
	f.Fuzz(func(t *testing.T, input string) {
		node, err := parseFilterExpression(input, 50)
		if err != nil {
			checkError(t, err)
			return
		}
		if nodes := countNodes(node); nodes > 50 {
			t.Fatalf("%d nodes over the cap of 50", nodes)
		}
		expr, err := node.render(e)
		if err != nil {
			checkError(t, err)
			return
		}
		checkSQL(t, expr.sql, expr.args)
	})
}

func countNodes(n *filterNode) int {
	count := 1
	for _, child := range n.children {
		count += countNodes(child)
	}
	return count
}

func FuzzParseFilterToken(f *testing.F) {
	for _, seed := range []string{"Milo", "$gt:5", "$or:$not:$eq:7", "$not:$or:$lt:3", "$null", "$eq:12:30", "$eq:$gt", "$ilike:a $ilike:b", "", ":", "$in:1,,2"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		token := parseFilterToken(raw)
		if !isOperator(string(token.operator)) {
			t.Fatalf("unknown operator %q", token.operator)
		}
		if token.comparator != OpAnd && token.comparator != OpOr {
			t.Fatalf("unknown comparator %q", token.comparator)
		}
		if token.operator != OpNull && !strings.HasSuffix(raw, token.value) {
			t.Fatalf("value %q is not the rest of %q", token.value, raw)
		}
	})
}

func FuzzParseSortBy(f *testing.F) {
	for _, seed := range []string{"name:asc", "name:DESC", "name", "name:up", "a:asc:desc", ":asc", "名前:desc"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		sortBy, err := parseSortBy([]string{value, value})
		if err != nil {
			checkError(t, err)
			return
		}
		if len(sortBy) != 1 {
			t.Fatalf("a repeated field must stay one key: %v", sortBy)
		}
		if order := sortBy[0].Order; order != Asc && order != Desc {
			t.Fatalf("order %q", order)
		}
	})
}

// fuzzRows are rows with every field of fuzzConfig, so cursors can be made from them.
var fuzzRows = []map[string]any{
	{"id": 1, "name": "a", "price": 2.5, "createdAt": "2024-01-01T00:00:00Z"},
	{"id": 2, "name": nil, "price": 3, "createdAt": "2024-01-02T00:00:00Z"},
}

func FuzzParseQuery(f *testing.F) {
	for _, seed := range []string{
		"page=2&limit=10&sortBy=name:asc&search=red+box&filter.price=$btw:1,10&filter.name=$or:$null",
		"filter=(name%3D$eq:a+OR+price%3D$gte:3)+AND+NOT+tags%3D$contains:x&searchBy=author",
		"limit=0&filter.tags=$contains:a,b",
		"limit=-1&withDeleted=true&paginationMode=disabled",
		"page=18446744073709551615&limit=100",
		"sortBy=id&filter.id=$in:" + strings.Repeat("1,", 50),
		"filter.unknown=1&sortBy=secret:asc",
		"search=%25_%5C&filter.name=$ilike:50%25",
		"limit=1&sortBy=price:desc&cursor=WyJuIiwicHJpY2U6REVTQyxpZDpBU0MiLDIuNSwxXQ",
		"cursor=%%%",
	} {
		f.Add(seed)
	}
	offset, cursor := fuzzEndpoints()
	f.Fuzz(func(t *testing.T, query string) {
		values, err := url.ParseQuery(query)
		if err != nil {
			return
		}
		for _, e := range []*Endpoint{offset, cursor} {
			req, err := ParseQuery(values, "/items")
			if err != nil {
				checkError(t, err)
				return
			}
			q, err := e.Prepare(req)
			if err != nil {
				checkError(t, err)
				continue
			}
			for _, condition := range q.Where {
				checkSQL(t, condition.SQL, condition.Args)
			}
			for _, order := range q.OrderBy(true) {
				checkSQL(t, order, nil)
			}
			if q.Limit == 0 || q.Limit > DefaultMaxLimit {
				t.Fatalf("limit %d out of range", q.Limit)
			}
			page, err := NewResponse(q, append([]map[string]any(nil), fuzzRows...), Total{Items: 42})
			if err != nil {
				t.Fatalf("answer: %v", err)
			}
			encoded, err := json.Marshal(page)
			if err != nil || !json.Valid(encoded) {
				t.Fatalf("answer JSON %s: %v", encoded, err)
			}
			if links := page.Meta.Links(); !strings.HasPrefix(links.Current, "/items?") {
				t.Fatalf("link %q", links.Current)
			}
		}
	})
}

func FuzzCursor(f *testing.F) {
	_, e := fuzzEndpoints()
	q, err := e.Prepare(&Request{SortBy: []Sort{{"price", Desc}}, Path: "/items"})
	if err != nil {
		f.Fatal(err)
	}
	page, err := NewResponse(q, append([]map[string]any(nil), fuzzRows...), Total{})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{page.Meta.NextCursor, "", "e30", "WyJuIl0", "WyJ4IiwicHJpY2U6REVTQyxpZDpBU0MiLDEsMl0", "WyJwIiwicHJpY2U6REVTQyxpZDpBU0MiLG51bGwsIngiXQ"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, cursor string) {
		q, err := e.Prepare(&Request{SortBy: []Sort{{"price", Desc}}, Cursor: cursor})
		if err != nil {
			checkError(t, err)
			return
		}
		for _, condition := range q.Where {
			checkSQL(t, condition.SQL, condition.Args)
		}
	})
}
