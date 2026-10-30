package paginate

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

// fieldFilter is the filter of one field: its conditions in request order.
type fieldFilter struct {
	field      *Field
	conditions []filterCondition
}

// filterCondition is one filter value of a field.
type filterCondition struct {
	// comparator joins the condition to the previous one of the field: $and or $or.
	comparator Operator
	not        bool
	operator   Operator
	// values holds one value; the comma-separated list of $in, $contains and $btw; none for $null.
	values []string
}

// filters checks the filter.<field> values of a request against the fields and parses them. Fields
// come out sorted by name, conditions in request order. Unknown fields, fields that are not
// filterable and operators that are not allowed fail, or are dropped with Lenient; everything else
// fails either way.
func (e *Endpoint) filters(req *Request) ([]fieldFilter, error) {
	cfg := &e.cfg
	filters := make([]fieldFilter, 0, len(req.Filter))
	conditions := uint64(0)
	for _, name := range slices.Sorted(maps.Keys(req.Filter)) {
		values := req.Filter[name]
		conditions += uint64(len(values))
		if conditions > cfg.MaxFilterConditions {
			return nil, invalidRequest(CodeTooManyValues, "", "too many filter values (more than %d)", cfg.MaxFilterConditions)
		}
		field, ok := e.fields[name]
		switch {
		case !ok && cfg.Lenient, ok && field.Filter == nil && cfg.Lenient:
			continue
		case !ok:
			return nil, invalidRequest(CodeUnknownField, name, "unknown filter field %s", name)
		case field.Filter == nil:
			return nil, invalidRequest(CodeFilterNotAllowed, name, "filter on %s is not allowed", name)
		}

		filter := fieldFilter{field: field}
		for _, raw := range values {
			condition, err := parseFilterCondition(raw, field, cfg.MaxFilterValues, false)
			var pe *Error
			if err != nil && cfg.Lenient && errors.As(err, &pe) && pe.Code == CodeOperatorNotAllowed {
				continue
			}
			if err != nil {
				return nil, err
			}
			filter.conditions = append(filter.conditions, condition)
		}
		if len(filter.conditions) != 0 {
			filters = append(filters, filter)
		}
	}
	return filters, nil
}

// parseFilterCondition parses one filter value of a field and checks it against the field's
// operators and type. Inside a filter= expression (inExpression) an empty value means "$eq empty
// string", as in nestjs-paginate, and $and/$or are refused: the expression itself joins its terms.
func parseFilterCondition(raw string, field *Field, maxValues uint64, inExpression bool) (filterCondition, error) {
	name := field.Name
	if raw == "" && !inExpression {
		return filterCondition{}, invalidRequest(CodeInvalidValue, name, "empty filter value for %s", name)
	}
	token := parseFilterToken(raw)
	if inExpression && token.hasComparator {
		return filterCondition{}, invalidRequest(CodeInvalidExpression, name, "$and/$or are not allowed in filter expressions, use AND/OR (%s)", name)
	}
	if !isAllowedOperator(token.operator, field.Filter) {
		return filterCondition{}, invalidRequest(CodeOperatorNotAllowed, name, "operator %s is not allowed for %s", token.operator, name)
	}

	condition := filterCondition{comparator: token.comparator, not: token.not, operator: token.operator}
	switch token.operator {
	case OpNull:
	case OpIn, OpContains, OpBtw:
		if uint64(strings.Count(token.value, ","))+1 > maxValues {
			return filterCondition{}, invalidRequest(CodeTooManyValues, name, "%s for %s has more than %d values", token.operator, name, maxValues)
		}
		condition.values = splitFilterList(token.value)
		if token.operator == OpBtw && !validRange(condition.values) {
			return filterCondition{}, invalidRequest(CodeInvalidValue, name, "%s for %s expects 1 or 2 non-empty values", token.operator, name)
		}
	default:
		condition.values = []string{token.value}
	}
	return condition, checkValues(field, condition)
}

// checkValues checks the values of a condition against the field's type. Patterns of $ilike and
// $sw are text whatever the type; $contains checks the element type of an array.
func checkValues(field *Field, condition filterCondition) error {
	t := field.Type
	switch {
	case condition.operator == OpILike || condition.operator == OpSw:
		return nil
	case condition.operator == OpContains:
		t = t.Elem()
	case t.IsArray():
		// $eq on an array column compares whole arrays; the database reads the literal.
		return nil
	}
	for _, value := range condition.values {
		if err := t.check(field.Name, value); err != nil {
			return err
		}
	}
	return nil
}

// splitFilterList splits a comma-separated list and trims the values: "$in:1, 2" is 1 and 2.
func splitFilterList(value string) []string {
	values := strings.Split(value, ",")
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return values
}

// validRange accepts a lower bound or both bounds of $btw.
func validRange(values []string) bool {
	switch len(values) {
	case 1:
		return values[0] != ""
	case 2:
		return values[0] != "" && values[1] != ""
	}
	return false
}
