package paginate

import (
	"reflect"
	"testing"
)

func TestFilterExpr(t *testing.T) {
	t.Parallel()
	and, or := OpAnd, OpOr
	eq := func(comparator Operator, value string) filterCondition {
		return filterCondition{comparator: comparator, operator: OpEq, values: []string{value}}
	}
	tests := []struct {
		name          string
		filters       []columnFilter
		compareAsText bool
		wantSQL       string
		wantArgs      []any
	}{
		{
			name:     "one condition",
			filters:  []columnFilter{{column: "t.name", conditions: []filterCondition{eq(and, "Milo")}}},
			wantSQL:  "t.name = ?",
			wantArgs: []any{"Milo"},
		},
		{
			name:          "compare as text",
			filters:       []columnFilter{{column: "t.id", conditions: []filterCondition{eq(and, "5")}}},
			compareAsText: true,
			wantSQL:       "CAST(t.id AS TEXT) = ?",
			wantArgs:      []any{"5"},
		},
		{
			name: "chain of one column: AND binds tighter than OR",
			filters: []columnFilter{{column: "t.id", conditions: []filterCondition{
				{comparator: and, operator: OpGt, values: []string{"3"}},
				{comparator: and, operator: OpLt, values: []string{"5"}},
				eq(or, "7"),
			}}},
			wantSQL:  "((t.id > ?) AND (t.id < ?) OR (t.id = ?))",
			wantArgs: []any{"3", "5", "7"},
		},
		{
			name:     "id=5 or id=7",
			filters:  []columnFilter{{column: "t.id", conditions: []filterCondition{eq(and, "5"), eq(or, "7")}}},
			wantSQL:  "((t.id = ?) OR (t.id = ?))",
			wantArgs: []any{"5", "7"},
		},
		{
			name: "columns are ANDed",
			filters: []columnFilter{
				{column: "t.a", conditions: []filterCondition{eq(and, "1")}},
				{column: "t.b", conditions: []filterCondition{eq(and, "2"), eq(and, "3")}},
			},
			wantSQL:  "((t.a = ?) AND ((t.b = ?) AND (t.b = ?)))",
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "columns starting with $or form one OR group",
			filters: []columnFilter{
				{column: "t.a", conditions: []filterCondition{eq(or, "1")}},
				{column: "t.b", conditions: []filterCondition{eq(or, "2")}},
				{column: "t.c", conditions: []filterCondition{eq(and, "3")}},
			},
			wantSQL:  "(((t.a = ?) OR (t.b = ?)) AND (t.c = ?))",
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "not",
			filters: []columnFilter{{column: "t.seen_at", conditions: []filterCondition{
				{comparator: and, not: true, operator: OpNull},
			}}},
			wantSQL: "NOT (t.seen_at IS NULL)",
		},
		{
			name: "in renders one placeholder per value",
			filters: []columnFilter{{column: "t.id", conditions: []filterCondition{
				{comparator: and, operator: OpIn, values: []string{"1", "2", "3"}},
			}}},
			wantSQL:  "t.id IN (?, ?, ?)",
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "between",
			filters: []columnFilter{{column: "t.age", conditions: []filterCondition{
				{comparator: and, operator: OpBtw, values: []string{"4", "6"}},
			}}},
			wantSQL:  "t.age >= ? AND t.age <= ?",
			wantArgs: []any{"4", "6"},
		},
		{
			name: "between with a lower bound only",
			filters: []columnFilter{{column: "t.age", conditions: []filterCondition{
				{comparator: and, operator: OpBtw, values: []string{"4"}},
			}}},
			wantSQL:  "t.age >= ?",
			wantArgs: []any{"4"},
		},
		{
			name: "not between keeps the range together",
			filters: []columnFilter{{column: "t.age", conditions: []filterCondition{
				{comparator: and, not: true, operator: OpBtw, values: []string{"4", "6"}},
			}}},
			wantSQL:  "NOT (t.age >= ? AND t.age <= ?)",
			wantArgs: []any{"4", "6"},
		},
		{
			name: "ilike and sw escape wildcards",
			filters: []columnFilter{{column: "t.name", conditions: []filterCondition{
				{comparator: and, operator: OpILike, values: []string{"50%_off"}},
				{comparator: and, operator: OpSw, values: []string{`a\b`}},
			}}},
			wantSQL:  "(CAST(t.name AS TEXT) ILIKE ?) AND (CAST(t.name AS TEXT) ILIKE ?)",
			wantArgs: []any{`%50\%\_off%`, `a\\b%`},
		},
		{
			name: "contains is array containment",
			filters: []columnFilter{{column: "t.roles", conditions: []filterCondition{
				{comparator: and, operator: OpContains, values: []string{"moderator", `a"b,c`}},
			}}},
			wantSQL:  "t.roles @> ?",
			wantArgs: []any{`{"moderator","a\"b,c"}`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := filterExpr(tc.filters, tc.compareAsText)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.sql != tc.wantSQL {
				t.Fatalf("sql:\nwant %s\ngot  %s", tc.wantSQL, got.sql)
			}
			if len(got.args) != 0 || len(tc.wantArgs) != 0 {
				if !reflect.DeepEqual(got.args, tc.wantArgs) {
					t.Fatalf("args: want %#v, got %#v", tc.wantArgs, got.args)
				}
			}
		})
	}
}

func TestFilterExprUnknownOperator(t *testing.T) {
	t.Parallel()
	_, err := filterExpr([]columnFilter{{column: "t.a", conditions: []filterCondition{{operator: "$regex", values: []string{"x"}}}}}, false)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestSearchExpr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		columns  []string
		terms    []string
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "one column",
			columns:  []string{"t.name"},
			terms:    []string{"milo"},
			wantSQL:  "CAST(t.name AS TEXT) ILIKE ?",
			wantArgs: []any{"%milo%"},
		},
		{
			name:     "columns are ORed",
			columns:  []string{"t.name", "t.email"},
			terms:    []string{"milo"},
			wantSQL:  "((CAST(t.name AS TEXT) ILIKE ?) OR (CAST(t.email AS TEXT) ILIKE ?))",
			wantArgs: []any{"%milo%", "%milo%"},
		},
		{
			name:     "words are ANDed",
			columns:  []string{"t.name", "t.email"},
			terms:    []string{"big", "red"},
			wantSQL:  "(((CAST(t.name AS TEXT) ILIKE ?) OR (CAST(t.email AS TEXT) ILIKE ?)) AND ((CAST(t.name AS TEXT) ILIKE ?) OR (CAST(t.email AS TEXT) ILIKE ?)))",
			wantArgs: []any{"%big%", "%big%", "%red%", "%red%"},
		},
		{
			name:     "wildcards are literal",
			columns:  []string{"t.name"},
			terms:    []string{"100%"},
			wantSQL:  "CAST(t.name AS TEXT) ILIKE ?",
			wantArgs: []any{`%100\%%`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := searchExpr(tc.columns, tc.terms)
			if got.sql != tc.wantSQL {
				t.Fatalf("sql:\nwant %s\ngot  %s", tc.wantSQL, got.sql)
			}
			if !reflect.DeepEqual(got.args, tc.wantArgs) {
				t.Fatalf("args: want %#v, got %#v", tc.wantArgs, got.args)
			}
		})
	}
}

func TestArrayLiteral(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		values []string
		want   string
	}{
		"plain":           {values: []string{"a", "b"}, want: `{"a","b"}`},
		"quotes":          {values: []string{`say "hi"`}, want: `{"say \"hi\""}`},
		"backslash":       {values: []string{`a\b`}, want: `{"a\\b"}`},
		"braces and null": {values: []string{"{x}", "NULL"}, want: `{"{x}","NULL"}`},
		"empty element":   {values: []string{""}, want: `{""}`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := arrayLiteral(tc.values); got != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestSearchColumnsAndTerms(t *testing.T) {
	t.Parallel()
	searchable := []string{"t.name", "t.email", "t.phone"}
	tests := []struct {
		name        string
		searchBy    []string
		ignore      bool
		multiWord   bool
		search      string
		wantColumns []string
		wantTerms   []string
	}{
		{name: "no searchBy: all columns", search: "a", wantColumns: searchable, wantTerms: []string{"a"}},
		{name: "searchBy narrows, keeps request order, drops duplicates and unknown", searchBy: []string{"t.phone", "t.name", "t.phone", "t.secret"}, search: "a", wantColumns: []string{"t.phone", "t.name"}, wantTerms: []string{"a"}},
		{name: "ignored searchBy keeps all columns", searchBy: []string{"t.phone"}, ignore: true, search: "a", wantColumns: searchable, wantTerms: []string{"a"}},
		{name: "whole term", search: " big  red ", wantColumns: searchable, wantTerms: []string{" big  red "}},
		{name: "multi-word", multiWord: true, search: " big  red ", wantColumns: searchable, wantTerms: []string{"big", "red"}},
		{name: "no search", search: "", wantColumns: searchable, wantTerms: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{SearchableColumns: searchable, IgnoreSearchByInQueryParam: tc.ignore, MultiWordSearch: tc.multiWord}
			if got := searchColumns(tc.searchBy, cfg); !reflect.DeepEqual(got, tc.wantColumns) {
				t.Fatalf("columns: want %v, got %v", tc.wantColumns, got)
			}
			if got := searchTerms(tc.search, cfg); !reflect.DeepEqual(got, tc.wantTerms) {
				t.Fatalf("terms: want %q, got %q", tc.wantTerms, got)
			}
		})
	}
}
