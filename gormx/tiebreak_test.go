package gormx

import (
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestTieBreakAllowed(t *testing.T) {
	t.Parallel()
	groupBy := func(columns ...clause.Column) map[string]clause.Clause {
		return map[string]clause.Clause{"GROUP BY": {Name: "GROUP BY", Expression: clause.GroupBy{Columns: columns}}}
	}
	tests := []struct {
		name   string
		stmt   *gorm.Statement
		column string
		want   bool
	}{
		{name: "nil statement", stmt: nil, column: "templates.id", want: false},
		{name: "empty column", stmt: &gorm.Statement{}, column: "", want: false},
		{name: "plain select", stmt: &gorm.Statement{}, column: "templates.id", want: true},
		{name: "distinct", stmt: &gorm.Statement{Distinct: true}, column: "templates.id", want: false},
		{
			name:   "group by the same qualified column",
			stmt:   &gorm.Statement{Clauses: groupBy(clause.Column{Name: "templates.id", Raw: true})},
			column: "templates.id",
			want:   true,
		},
		{
			name:   "group by table+name column",
			stmt:   &gorm.Statement{Clauses: groupBy(clause.Column{Table: "templates", Name: "id"})},
			column: "templates.id",
			want:   true,
		},
		{
			name:   "group by several columns in one raw clause",
			stmt:   &gorm.Statement{Clauses: groupBy(clause.Column{Name: "templates.title, templates.id", Raw: true})},
			column: "templates.id",
			want:   true,
		},
		{
			name:   "group by short column name",
			stmt:   &gorm.Statement{Clauses: groupBy(clause.Column{Name: "id"})},
			column: "templates.id",
			want:   true,
		},
		{
			name:   "group by another column",
			stmt:   &gorm.Statement{Clauses: groupBy(clause.Column{Name: "templates.user_id", Raw: true})},
			column: "templates.id",
			want:   false,
		},
		{
			name:   "group by with foreign expression",
			stmt:   &gorm.Statement{Clauses: map[string]clause.Clause{"GROUP BY": {Name: "GROUP BY", Expression: clause.Expr{SQL: "1"}}}},
			column: "templates.id",
			want:   false,
		},
		{
			name:   "group by the id of another table",
			stmt:   &gorm.Statement{Clauses: groupBy(clause.Column{Name: "authors.id", Raw: true})},
			column: "templates.id",
			want:   false,
		},
		{
			name:   "raw DISTINCT in Select",
			stmt:   &gorm.Statement{Selects: []string{"distinct templates.*"}},
			column: "templates.id",
			want:   false,
		},
		{
			name: "raw DISTINCT in a Select with arguments",
			stmt: &gorm.Statement{Clauses: map[string]clause.Clause{
				"SELECT": {Name: "SELECT", Expression: clause.Expr{SQL: "DISTINCT ON (templates.author_id) templates.*, ? AS x", Vars: []any{1}}},
			}},
			column: "templates.id",
			want:   false,
		},
		{
			name:   "plain Select",
			stmt:   &gorm.Statement{Selects: []string{"templates.id", "templates.title"}},
			column: "templates.id",
			want:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tieBreakAllowed(tc.stmt, tc.column); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}
