package paginate

import "slices"

// Operator is a filter operator, the $not suffix or a comparator of a filter value.
type Operator string

const (
	// OpEq is equality, the default operator: filter.name=Milo is filter.name=$eq:Milo.
	OpEq Operator = "$eq"
	// OpGt is "greater than".
	OpGt Operator = "$gt"
	// OpGte is "greater than or equal".
	OpGte Operator = "$gte"
	// OpIn is "one of" a comma-separated list: $in:1,2,3.
	OpIn Operator = "$in"
	// OpNull is IS NULL; it takes no value.
	OpNull Operator = "$null"
	// OpLt is "less than".
	OpLt Operator = "$lt"
	// OpLte is "less than or equal".
	OpLte Operator = "$lte"
	// OpBtw is an inclusive range of two comma-separated values: $btw:1,10. One value is a lower
	// bound only.
	OpBtw Operator = "$btw"
	// OpILike is a case-insensitive substring match.
	OpILike Operator = "$ilike"
	// OpSw is a case-insensitive prefix match ("starts with").
	OpSw Operator = "$sw"
	// OpContains matches an array column that contains every comma-separated value.
	OpContains Operator = "$contains"

	// OpNot negates the operator after it: $not:$null, $not:$in:1,2.
	OpNot Operator = "$not"

	// OpAnd joins a filter.<field> value to the previous value of the same field with AND; it is
	// the default.
	OpAnd Operator = "$and"
	// OpOr joins a filter.<field> value to the previous value of the same field with OR.
	OpOr Operator = "$or"
)

// AllOperators allows every operator on a column, like `true` in nestjs-paginate.
var AllOperators = []Operator{OpEq, OpGt, OpGte, OpIn, OpNull, OpLt, OpLte, OpBtw, OpILike, OpSw, OpContains}

func isOperator(value string) bool {
	return slices.Contains(AllOperators, Operator(value))
}

func isSuffix(value string) bool {
	return value == string(OpNot)
}

func isComparator(value string) bool {
	return value == string(OpAnd) || value == string(OpOr)
}

// isAllowedOperator checks an operator against the list of a column; $eq is always allowed, as in
// nestjs-paginate, so an empty list allows $eq only.
func isAllowedOperator(operator Operator, allowed []Operator) bool {
	return operator == OpEq || slices.Contains(allowed, operator)
}
