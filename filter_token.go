package paginate

import "strings"

// filterToken is one parsed filter value: "[$or|$and:][$not:][$operator:]value".
type filterToken struct {
	comparator    Operator
	hasComparator bool
	not           bool
	operator      Operator
	value         string
}

// parseFilterToken splits a filter value the way nestjs-paginate does: up to three prefixes, a
// comparator ($and by default, or $or), the $not suffix and an operator ($eq by default), in any
// order and each at most once, then the value. Everything after the prefixes is the value, so it
// may contain ":" or operator-like text: "$eq:12:30" is "12:30", "$eq:$gt" is "$gt". $null takes
// no value.
func parseFilterToken(raw string) filterToken {
	token := filterToken{comparator: OpAnd, operator: OpEq}
	var hasSuffix, hasOperator bool
	rest := raw
prefixes:
	for rest != "" {
		head, tail, _ := strings.Cut(rest, ":")
		switch {
		case !token.hasComparator && isComparator(head):
			token.hasComparator = true
			token.comparator = Operator(head)
		case !hasSuffix && isSuffix(head):
			hasSuffix = true
			token.not = true
		case !hasOperator && isOperator(head):
			hasOperator = true
			token.operator = Operator(head)
		default:
			break prefixes
		}
		rest = tail
	}
	token.value = rest
	if token.operator == OpNull {
		token.value = ""
	}
	return token
}
