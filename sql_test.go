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
		filters       []fieldFilter
		compareAsText bool
		wantSQL       string
		wantArgs      []any
	}{
		{
			name:     "one condition",
			filters:  []fieldFilter{{field: col("t.name"), conditions: []filterCondition{eq(and, "Milo")}}},
			wantSQL:  "t.name = ?",
			wantArgs: []any{"Milo"},
		},
		{
			name:          "compare as text",
			filters:       []fieldFilter{{field: col("t.id"), conditions: []filterCondition{eq(and, "5")}}},
			compareAsText: true,
			wantSQL:       "CAST(t.id AS TEXT) = ?",
			wantArgs:      []any{"5"},
		},
		{
			name: "chain of one column: AND binds tighter than OR",
			filters: []fieldFilter{{field: col("t.id"), conditions: []filterCondition{
				{comparator: and, operator: OpGt, values: []string{"3"}},
				{comparator: and, operator: OpLt, values: []string{"5"}},
				eq(or, "7"),
			}}},
			wantSQL:  "((t.id > ?) AND (t.id < ?) OR (t.id = ?))",
			wantArgs: []any{"3", "5", "7"},
		},
		{
			name:     "id=5 or id=7",
			filters:  []fieldFilter{{field: col("t.id"), conditions: []filterCondition{eq(and, "5"), eq(or, "7")}}},
			wantSQL:  "((t.id = ?) OR (t.id = ?))",
			wantArgs: []any{"5", "7"},
		},
		{
			name: "columns are ANDed",
			filters: []fieldFilter{
				{field: col("t.a"), conditions: []filterCondition{eq(and, "1")}},
				{field: col("t.b"), conditions: []filterCondition{eq(and, "2"), eq(and, "3")}},
			},
			wantSQL:  "((t.a = ?) AND ((t.b = ?) AND (t.b = ?)))",
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "columns starting with $or form one OR group",
			filters: []fieldFilter{
				{field: col("t.a"), conditions: []filterCondition{eq(or, "1")}},
				{field: col("t.b"), conditions: []filterCondition{eq(or, "2")}},
				{field: col("t.c"), conditions: []filterCondition{eq(and, "3")}},
			},
			wantSQL:  "(((t.a = ?) OR (t.b = ?)) AND (t.c = ?))",
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "not",
			filters: []fieldFilter{{field: col("t.seen_at"), conditions: []filterCondition{
				{comparator: and, not: true, operator: OpNull},
			}}},
			wantSQL: "NOT (t.seen_at IS NULL)",
		},
		{
			name: "in renders one placeholder per value",
			filters: []fieldFilter{{field: col("t.id"), conditions: []filterCondition{
				{comparator: and, operator: OpIn, values: []string{"1", "2", "3"}},
			}}},
			wantSQL:  "t.id IN (?, ?, ?)",
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "between",
			filters: []fieldFilter{{field: col("t.age"), conditions: []filterCondition{
				{comparator: and, operator: OpBtw, values: []string{"4", "6"}},
			}}},
			wantSQL:  "t.age >= ? AND t.age <= ?",
			wantArgs: []any{"4", "6"},
		},
		{
			name: "between with a lower bound only",
			filters: []fieldFilter{{field: col("t.age"), conditions: []filterCondition{
				{comparator: and, operator: OpBtw, values: []string{"4"}},
			}}},
			wantSQL:  "t.age >= ?",
			wantArgs: []any{"4"},
		},
		{
			name: "not between keeps the range together",
			filters: []fieldFilter{{field: col("t.age"), conditions: []filterCondition{
				{comparator: and, not: true, operator: OpBtw, values: []string{"4", "6"}},
			}}},
			wantSQL:  "NOT (t.age >= ? AND t.age <= ?)",
			wantArgs: []any{"4", "6"},
		},
		{
			name: "ilike and sw escape wildcards",
			filters: []fieldFilter{{field: col("t.name"), conditions: []filterCondition{
				{comparator: and, operator: OpILike, values: []string{"50%_off"}},
				{comparator: and, operator: OpSw, values: []string{`a\b`}},
			}}},
			wantSQL:  "(CAST(t.name AS TEXT) ILIKE ?) AND (CAST(t.name AS TEXT) ILIKE ?)",
			wantArgs: []any{`%50\%\_off%`, `a\\b%`},
		},
		{
			name: "contains is array containment",
			filters: []fieldFilter{{field: col("t.roles"), conditions: []filterCondition{
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
	_, err := filterExpr([]fieldFilter{{field: col("t.a"), conditions: []filterCondition{{operator: "$regex", values: []string{"x"}}}}}, false)
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

func TestSearchTerms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		multiWord bool
		search    string
		want      []string
	}{
		{name: "whole term", search: " big  red ", want: []string{" big  red "}},
		{name: "multi-word", multiWord: true, search: " big  red ", want: []string{"big", "red"}},
		{name: "no search", search: "", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := searchTerms(tc.search, tc.multiWord); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("terms: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSearchFields(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Fields: []Field{
			{Name: "name", Column: "t.name", Searchable: true},
			{Name: "email", Column: "t.email", Searchable: true},
			{Name: "phone", Column: "t.phone", Searchable: true},
			{Name: "secret", Column: "t.secret"},
		},
		TieBreak: "t.id",
	}
	tests := []struct {
		name     string
		searchBy []string
		ignore   bool
		lenient  bool
		want     []string
	}{
		{name: "no searchBy: every searchable field", want: []string{"name", "email", "phone"}},
		{name: "searchBy narrows, keeps request order, drops duplicates", searchBy: []string{"phone", "name", "phone"}, want: []string{"phone", "name"}},
		{name: "Lenient drops fields that are not searchable", searchBy: []string{"phone", "secret", "unknown"}, lenient: true, want: []string{"phone"}},
		{name: "ignored searchBy keeps every field", searchBy: []string{"phone"}, ignore: true, want: []string{"name", "email", "phone"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := cfg
			cfg.IgnoreSearchByInQueryParam, cfg.Lenient = tc.ignore, tc.lenient
			e := MustCompile(cfg)
			fields, err := e.searchFields(&Request{SearchBy: tc.searchBy})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, field := range fields {
				got = append(got, field.Name)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// col is a field reading column, for rendering tests.
func col(column string) *Field {
	return &Field{Name: column, Column: column}
}
