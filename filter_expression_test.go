package paginate

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// String prints a tree compactly for assertions: and(a="$eq:1", not(b="$null")).
func (n *filterNode) String() string {
	switch n.kind {
	case filterTerm:
		return fmt.Sprintf("%s=%q", n.field, n.value)
	case filterNot:
		return "not(" + n.children[0].String() + ")"
	}
	parts := make([]string, 0, len(n.children))
	for _, child := range n.children {
		parts = append(parts, child.String())
	}
	name := "and"
	if n.kind == filterOr {
		name = "or"
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

// The cases follow the parser tests of nestjs-paginate 16.0.1 (src/filter-expression.spec.ts).
func TestParseFilterExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{input: `name=$eq:Milo`, want: `name="$eq:Milo"`},
		{input: `home.config.theme=$not:$in:a,b,c`, want: `home.config.theme="$not:$in:a,b,c"`},
		{input: `a=$eq:1 AND b=$eq:2 OR c=$eq:3`, want: `or(and(a="$eq:1", b="$eq:2"), c="$eq:3")`},
		{input: `(a=$eq:1 OR b=$eq:2) AND c=$eq:3`, want: `and(or(a="$eq:1", b="$eq:2"), c="$eq:3")`},
		{input: `a=$eq:1 and b=$eq:2 Or c=$eq:3`, want: `or(and(a="$eq:1", b="$eq:2"), c="$eq:3")`},
		{input: `NOT name=$eq:Leche`, want: `not(name="$eq:Leche")`},
		{input: `(b=$eq:4 AND a.c=$lt:7)`, want: `and(b="$eq:4", a.c="$lt:7")`},
		{input: `home.name=$eq:"Cat Mansion" AND name=$eq:Milo`, want: `and(home.name="$eq:Cat Mansion", name="$eq:Milo")`},
		{input: `home.name=$eq:'Cat Mansion'`, want: `home.name="$eq:Cat Mansion"`},
		{input: `name=$eq:foo" bar"baz`, want: `name="$eq:foo barbaz"`},
		{input: `name=$eq:"a AND b"`, want: `name="$eq:a AND b"`},
		{input: `name=$eq:"Milo \"the cat\""`, want: `name="$eq:Milo \"the cat\""`},
		{input: `name=$eq:'it\'s'`, want: `name="$eq:it's"`},
		{input: `name=$eq:"a\"b\'c\\d"`, want: `name="$eq:a\"b'c\\d"`},
		{input: `name=$eq:"C:\Users\d"`, want: `name="$eq:C:\\Users\\d"`},
		{input: `name=$eq:"O'Malley"`, want: `name="$eq:O'Malley"`},
		{input: `name=$eq:'Milo "the cat"'`, want: `name="$eq:Milo \"the cat\""`},
		{input: `name=$eq:a'"'b"'"c`, want: `name="$eq:a\"b'c"`},
		{input: `name=$eq:"say ""hi"""`, want: `name="$eq:say hi"`},
		{input: `name=$eq:a=b`, want: `name="$eq:a=b"`},
		{input: `name=`, want: `name=""`},
		{input: `NOT a=$eq:1 AND b=$eq:2`, want: `and(not(a="$eq:1"), b="$eq:2")`},
		{input: `NOT (a=$eq:1 OR b=$eq:2)`, want: `not(or(a="$eq:1", b="$eq:2"))`},
		{input: `((a=$eq:1 OR b=$eq:2) AND c=$eq:3)`, want: `and(or(a="$eq:1", b="$eq:2"), c="$eq:3")`},
		{input: "a=$eq:1\tAND\nb=$eq:2", want: `and(a="$eq:1", b="$eq:2")`},
		{input: `toys.name=$none:$not:$eq:String`, want: `toys.name="$none:$not:$eq:String"`},
		{input: `a=1 AND b=2 AND c=3`, want: `and(a="1", b="2", c="3")`},
		{input: `NOT NOT a=1`, want: `not(not(a="1"))`},
		{input: `title=$eq:"(draft)"`, want: `title="$eq:(draft)"`},
		{input: `name=$eq:кот OR name=$sw:"кот "`, want: `or(name="$eq:кот", name="$sw:кот ")`},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, err := parseFilterExpression(tc.input, DefaultMaxFilterExpressionComplexity)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := node.String(); got != tc.want {
				t.Fatalf("want %s\ngot  %s", tc.want, got)
			}
		})
	}
}

func TestParseFilterExpressionErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{input: ``, want: "empty filter expression"},
		{input: `   `, want: "empty filter expression"},
		{input: `name=$eq:"unterminated`, want: "unterminated quote"},
		{input: `name=$eq:'unterminated`, want: "unterminated quote"},
		{input: `(a=$eq:1`, want: `expected ")"`},
		{input: `justacolumn`, want: `expected "field=value"`},
		{input: `=novalue`, want: `expected "field=value"`},
		{input: `a=$eq:1 AND`, want: "unexpected end"},
		{input: `NOT`, want: "unexpected end"},
		{input: `a=$eq:1 b=$eq:2`, want: "after the end"},
		{input: `a=$eq:1)`, want: "after the end"},
		{input: `OR a=$eq:1`, want: `unexpected "OR"`},
		{input: `)`, want: `unexpected ")"`},
		{input: `()`, want: `unexpected ")"`},
		{input: `a=$eq:1 AND AND b=$eq:2`, want: `unexpected "AND"`},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			_, err := parseFilterExpression(tc.input, DefaultMaxFilterExpressionComplexity)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("want validation error with %q, got %v", tc.want, err)
			}
		})
	}
}

func TestFilterExpressionComplexity(t *testing.T) {
	t.Parallel()
	// N terms ANDed are 2N-1 nodes: every term and every AND counts.
	chain := func(terms int) string {
		parts := make([]string, terms)
		for i := range parts {
			parts[i] = fmt.Sprintf("c%d=$eq:%d", i, i)
		}
		return strings.Join(parts, " AND ")
	}
	tests := []struct {
		name    string
		input   string
		max     uint64
		wantErr bool
	}{
		{name: "exactly at the cap", input: chain(2), max: 3},
		{name: "one node over", input: chain(2), max: 2, wantErr: true},
		{name: "NOT counts", input: `NOT NOT a=$eq:1`, max: 2, wantErr: true},
		{name: "parentheses count, so deep nesting is caught", input: `(((a=$eq:1)))`, max: 3, wantErr: true},
		{name: "the default cap", input: chain(int(DefaultMaxFilterExpressionComplexity)), max: DefaultMaxFilterExpressionComplexity, wantErr: true},
		{name: "deep nesting stops early", input: strings.Repeat("(", 100000) + "a=1" + strings.Repeat(")", 100000), max: 100, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseFilterExpression(tc.input, tc.max)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "too complex") {
					t.Fatalf("want a complexity error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func expressionEndpoint(lenient bool) *Endpoint {
	return MustCompile(Config{
		Fields: []Field{
			{Name: "name", Column: "t.name", Filter: AllOperators},
			{Name: "age", Column: "t.age", Type: Int, Filter: []Operator{OpGte, OpLt, OpBtw}},
			{Name: "status", Column: "t.status", Filter: []Operator{OpIn}},
			{Name: "tags", Column: "t.tags", Filter: []Operator{OpContains}},
			{Name: "secret", Column: "t.secret"},
		},
		TieBreak: "t.id",
		Lenient:  lenient,
	})
}

func TestRenderFilterExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		strict   bool
		wantSQL  string
		wantArgs []any
		wantErr  string
	}{
		{
			name:     "or, and, not",
			input:    `(status=$in:paid,shipped OR age=$gte:18) AND NOT name=$null`,
			strict:   true,
			wantSQL:  "(((t.status IN (?, ?)) OR (t.age >= ?)) AND (NOT (t.name IS NULL)))",
			wantArgs: []any{"paid", "shipped", "18"},
		},
		{
			name:     "a term alone",
			input:    `tags=$contains:red,big`,
			strict:   true,
			wantSQL:  "t.tags @> ?",
			wantArgs: []any{`{"red","big"}`},
		},
		{
			name:     "value suffix and expression NOT",
			input:    `NOT name=$not:$eq:x`,
			strict:   true,
			wantSQL:  "NOT (NOT (t.name = ?))",
			wantArgs: []any{"x"},
		},
		{
			name:     "empty value is $eq empty string",
			input:    `name=`,
			strict:   true,
			wantSQL:  "t.name = ?",
			wantArgs: []any{""},
		},
		{
			name:     "quoted value with spaces and wildcards",
			input:    `name=$ilike:"50% off"`,
			strict:   true,
			wantSQL:  "CAST(t.name AS TEXT) ILIKE ?",
			wantArgs: []any{`%50\% off%`},
		},
		{name: "unknown field fails even with Lenient", input: `password=$eq:x OR name=$eq:y`, wantErr: "unknown filter field password"},
		{name: "field that is not filterable", input: `secret=$eq:x`, strict: true, wantErr: "filter on secret is not allowed"},
		{name: "operator that is not allowed fails even with Lenient", input: `age=$ilike:1`, wantErr: "operator $ilike is not allowed for age"},
		{name: "comparators belong to filter.<field>", input: `name=$or:x`, strict: true, wantErr: "$and/$or are not allowed"},
		{name: "malformed list", input: `age=$btw:1,2,3`, strict: true, wantErr: "expects 1 or 2 non-empty values"},
		{name: "value of the wrong type", input: `age=$gte:old OR name=x`, strict: true, wantErr: `invalid value "old" for age`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node, err := parseFilterExpression(tc.input, DefaultMaxFilterExpressionComplexity)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := node.render(expressionEndpoint(!tc.strict))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("want validation error with %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.sql != tc.wantSQL {
				t.Fatalf("sql:\nwant %s\ngot  %s", tc.wantSQL, got.sql)
			}
			if !reflect.DeepEqual(got.args, tc.wantArgs) {
				t.Fatalf("args: want %#v, got %#v", tc.wantArgs, got.args)
			}
		})
	}
}
