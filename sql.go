package paginate

import "strings"

// Condition is a SQL condition with "?" placeholders and one argument per placeholder, never a
// slice. It is safe to AND with other conditions: a top-level OR is parenthesized. Column names
// come from the Config only; every value is an argument.
type Condition struct {
	SQL  string
	Args []any
}

// sqlExpr is a condition under construction.
type sqlExpr struct {
	sql  string
	args []any
	// grouped marks SQL wrapped in its own parentheses.
	grouped bool
}

// group returns the SQL wrapped in parentheses, once.
func (e sqlExpr) group() string {
	if e.grouped {
		return e.sql
	}
	return "(" + e.sql + ")"
}

// searchExpr matches every term in at least one column: columns are ORed per term, terms ANDed.
func searchExpr(columns []string, terms []string) sqlExpr {
	termExprs := make([]sqlExpr, 0, len(terms))
	for _, term := range terms {
		pattern := containsPattern(term)
		columnExprs := make([]sqlExpr, 0, len(columns))
		for _, column := range columns {
			columnExprs = append(columnExprs, sqlExpr{sql: textColumn(column) + " ILIKE ?", args: []any{pattern}})
		}
		termExprs = append(termExprs, joinExprs(columnExprs, " OR "))
	}
	return joinExprs(termExprs, " AND ")
}

// filterExpr joins filter.<field> conditions the way nestjs-paginate (up to 14) does: the
// conditions of a column form a chain in request order, each joined to the previous one by its
// own $and or $or, with SQL precedence (AND binds tighter). Columns whose first condition is $or
// are ORed into one group; the group and every other column are ANDed.
//
//	filter.id=$gt:3&filter.id=$lt:5&filter.id=$or:$eq:7  →  (id > 3 AND id < 5 OR id = 7)
//	filter.a=$or:1&filter.b=$or:2&filter.c=3             →  (a = 1 OR b = 2) AND c = 3
func filterExpr(filters []columnFilter, compareAsText bool) (sqlExpr, error) {
	orGroup := make([]sqlExpr, 0, len(filters))
	andGroup := make([]sqlExpr, 0, len(filters))
	for _, filter := range filters {
		chain, err := chainExpr(filter, compareAsText)
		if err != nil {
			return sqlExpr{}, err
		}
		if filter.conditions[0].comparator == OpOr {
			orGroup = append(orGroup, chain)
		} else {
			andGroup = append(andGroup, chain)
		}
	}

	parts := make([]sqlExpr, 0, len(andGroup)+1)
	if len(orGroup) != 0 {
		parts = append(parts, joinExprs(orGroup, " OR "))
	}
	parts = append(parts, andGroup...)
	return joinExprs(parts, " AND "), nil
}

// chainExpr joins the conditions of a column; the comparator of the first one is not used here.
func chainExpr(filter columnFilter, compareAsText bool) (sqlExpr, error) {
	var sql strings.Builder
	args := make([]any, 0, len(filter.conditions))
	hasOr := false
	for i, condition := range filter.conditions {
		expr, err := conditionExpr(filter.column, condition, compareAsText)
		if err != nil {
			return sqlExpr{}, err
		}
		if i > 0 {
			if condition.comparator == OpOr {
				hasOr = true
				sql.WriteString(" OR ")
			} else {
				sql.WriteString(" AND ")
			}
		}
		if len(filter.conditions) > 1 {
			sql.WriteString("(" + expr.sql + ")")
		} else {
			sql.WriteString(expr.sql)
		}
		args = append(args, expr.args...)
	}
	if hasOr {
		return sqlExpr{sql: "(" + sql.String() + ")", args: args, grouped: true}, nil
	}
	return sqlExpr{sql: sql.String(), args: args}, nil
}

// conditionExpr renders one condition. Values are always arguments; the column comes from the
// config.
func conditionExpr(column string, condition filterCondition, compareAsText bool) (sqlExpr, error) {
	expr, err := operatorExpr(column, condition, compareAsText)
	if err != nil {
		return sqlExpr{}, err
	}
	if condition.not {
		expr.sql = "NOT (" + expr.sql + ")"
	}
	return expr, nil
}

func operatorExpr(column string, condition filterCondition, compareAsText bool) (sqlExpr, error) {
	values := condition.values
	equated := column
	if compareAsText {
		equated = textColumn(column)
	}
	switch condition.operator {
	case OpNull:
		return sqlExpr{sql: column + " IS NULL"}, nil
	case OpEq:
		return sqlExpr{sql: equated + " = ?", args: []any{values[0]}}, nil
	case OpIn:
		return sqlExpr{sql: equated + " IN (" + placeholders(len(values)) + ")", args: anyArgs(values)}, nil
	case OpGt:
		return sqlExpr{sql: column + " > ?", args: []any{values[0]}}, nil
	case OpGte:
		return sqlExpr{sql: column + " >= ?", args: []any{values[0]}}, nil
	case OpLt:
		return sqlExpr{sql: column + " < ?", args: []any{values[0]}}, nil
	case OpLte:
		return sqlExpr{sql: column + " <= ?", args: []any{values[0]}}, nil
	case OpBtw:
		if len(values) == 1 {
			return sqlExpr{sql: column + " >= ?", args: []any{values[0]}}, nil
		}
		return sqlExpr{sql: column + " >= ? AND " + column + " <= ?", args: []any{values[0], values[1]}}, nil
	case OpILike:
		return sqlExpr{sql: textColumn(column) + " ILIKE ?", args: []any{containsPattern(values[0])}}, nil
	case OpSw:
		return sqlExpr{sql: textColumn(column) + " ILIKE ?", args: []any{prefixPattern(values[0])}}, nil
	case OpContains:
		return sqlExpr{sql: column + " @> ?", args: []any{arrayLiteral(values)}}, nil
	}
	return sqlExpr{}, invalidRequest("unknown operator %s", condition.operator)
}

// joinExprs joins expressions with sep, parenthesizing each part and the whole when there are
// several.
func joinExprs(exprs []sqlExpr, sep string) sqlExpr {
	if len(exprs) == 1 {
		return exprs[0]
	}
	parts := make([]string, 0, len(exprs))
	args := make([]any, 0, len(exprs))
	for _, expr := range exprs {
		parts = append(parts, expr.group())
		args = append(args, expr.args...)
	}
	return sqlExpr{sql: "(" + strings.Join(parts, sep) + ")", args: args, grouped: true}
}

// textColumn casts a column to text for text matching, so it works on numbers, uuids and enums too.
func textColumn(column string) string {
	return "CAST(" + column + " AS TEXT)"
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func anyArgs(values []string) []any {
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}
	return args
}

// arrayElementEscaper escapes a value inside a double-quoted Postgres array element.
var arrayElementEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// arrayLiteral renders values as a Postgres array literal with every element quoted, so commas,
// braces and quotes stay part of the values. Sent as text, it takes the array type of the column:
// '{"a","b"}' is text[] for a text[] column and int[] for an int[] one.
func arrayLiteral(values []string) string {
	var literal strings.Builder
	literal.WriteByte('{')
	for i, value := range values {
		if i > 0 {
			literal.WriteByte(',')
		}
		literal.WriteByte('"')
		literal.WriteString(arrayElementEscaper.Replace(value))
		literal.WriteByte('"')
	}
	literal.WriteByte('}')
	return literal.String()
}
