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
	cfg := NewConfig("t", []string{"t.id", "t.name", "t.price", "t.created_at"}, map[string]string{
		"id":        "t.id",
		"name":      "t.name",
		"price":     "t.price",
		"tags":      "t.tags",
		"createdAt": "t.created_at",
		"author":    "a.name",
	})
	cfg.SearchableColumns = []string{"t.name", "a.name"}
	cfg.FilterableColumns = FilterableColumns{
		"t.id":         AllOperators,
		"t.name":       AllOperators,
		"t.price":      AllOperators,
		"t.tags":       AllOperators,
		"t.created_at": AllOperators,
		"a.name":       {},
	}
	cfg.TieBreakColumn = "t.id"
	cfg.MultiWordSearch = true
	cfg.AllowDisabledPagination = true
	cfg.AllowWithDeletedInQuery = true
	cfg.Format = FormatNestJS
	return *cfg
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
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("client input must fail with ErrInvalidRequest, got %v", err)
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
	cfg := fuzzConfig()
	if err := normalizeConfig(&cfg); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, input string) {
		node, err := parseFilterExpression(input, 50)
		if err != nil {
			checkError(t, err)
			return
		}
		if nodes := countNodes(node); nodes > 50 {
			t.Fatalf("%d nodes over the cap of 50", nodes)
		}
		expr, err := node.render(&cfg)
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
		sortBy, keys, err := parseSortBy([]string{value, value})
		if err != nil {
			checkError(t, err)
			return
		}
		if len(keys) != 1 || len(sortBy) != 1 {
			t.Fatalf("a repeated key must stay one key: %v %v", keys, sortBy)
		}
		if order := sortBy[keys[0]]; order != Asc && order != Desc {
			t.Fatalf("order %q", order)
		}
	})
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
	} {
		f.Add(seed)
	}
	cfg := fuzzConfig()
	f.Fuzz(func(t *testing.T, query string) {
		values, err := url.ParseQuery(query)
		if err != nil {
			return
		}
		req, err := ParseQuery(values, "/items")
		if err != nil {
			checkError(t, err)
			return
		}
		q, err := Prepare(req, cfg)
		if err != nil {
			checkError(t, err)
			return
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
		encoded, err := json.Marshal(NewResponse([]int{1}, *req, 42))
		if err != nil {
			t.Fatalf("answer: %v", err)
		}
		if !json.Valid(encoded) {
			t.Fatalf("invalid JSON %s", encoded)
		}
		if links := NewResponse([]int{1}, *req, 42).Meta.Links(); !strings.HasPrefix(links.Current, "/items?page=") {
			t.Fatalf("link %q", links.Current)
		}
	})
}
