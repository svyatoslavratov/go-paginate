package paginate

import (
	"maps"
	"slices"
	"strings"
)

// fieldValues are the filter.<field> values of one column, with the API field they came from for
// error messages: messages name what the client sent, never the schema.
type fieldValues struct {
	field  string
	values []string
}

// columnFilter is the filter of one column: its conditions in request order.
type columnFilter struct {
	column     string
	conditions []filterCondition
}

// filterCondition is one filter value of a column.
type filterCondition struct {
	// comparator joins the condition to the previous one of the column: $and or $or.
	comparator Operator
	not        bool
	operator   Operator
	// values holds one value; the comma-separated list of $in, $contains and $btw; none for $null.
	values []string
}

// parseFilters checks filter.<field> values, keyed by column, against FilterableColumns and
// parses them. Columns come out sorted, conditions in request order. Columns and operators that
// are not allowed fail with StrictValidate and are dropped without it; a list longer than
// MaxFilterValues fails always.
func parseFilters(filter map[string]*fieldValues, cfg *Config) ([]columnFilter, error) {
	filters := make([]columnFilter, 0, len(filter))
	for _, column := range slices.Sorted(maps.Keys(filter)) {
		input := filter[column]
		allowed, filterable := cfg.FilterableColumns[column]
		if !filterable {
			if cfg.StrictValidate {
				return nil, invalidRequest("filter on %s is not allowed", input.field)
			}
			continue
		}

		parsed := columnFilter{column: column}
		for _, raw := range input.values {
			condition, overLimit, err := parseFilterCondition(raw, input.field, allowed, cfg.MaxFilterValues, false)
			if err != nil {
				if cfg.StrictValidate || overLimit {
					return nil, err
				}
				continue
			}
			parsed.conditions = append(parsed.conditions, condition)
		}
		if len(parsed.conditions) != 0 {
			filters = append(filters, parsed)
		}
	}
	return filters, nil
}

// parseFilterCondition parses one filter value of the API field. overLimit marks an error
// non-strict mode does not forgive. Inside a filter= expression (inExpression) an empty value
// means "$eq empty string", as in nestjs-paginate, and $and/$or are refused: the expression
// itself joins its terms.
func parseFilterCondition(raw, field string, allowed []Operator, maxValues uint64, inExpression bool) (condition filterCondition, overLimit bool, err error) {
	if raw == "" && !inExpression {
		return filterCondition{}, false, invalidRequest("empty filter value for %s", field)
	}
	token := parseFilterToken(raw)
	if inExpression && token.hasComparator {
		return filterCondition{}, false, invalidRequest("$and/$or are not allowed in filter expressions, use AND/OR (%s)", field)
	}
	if !isAllowedOperator(token.operator, allowed) {
		return filterCondition{}, false, invalidRequest("operator %s is not allowed for %s", token.operator, field)
	}

	condition = filterCondition{comparator: token.comparator, not: token.not, operator: token.operator}
	switch token.operator {
	case OpNull:
	case OpIn, OpContains, OpBtw:
		if uint64(strings.Count(token.value, ","))+1 > maxValues {
			return filterCondition{}, true, invalidRequest("%s for %s has more than %d values", token.operator, field, maxValues)
		}
		condition.values = splitFilterList(token.value)
		if token.operator == OpBtw && !validRange(condition.values) {
			return filterCondition{}, false, invalidRequest("%s for %s expects 1 or 2 non-empty values", token.operator, field)
		}
	default:
		condition.values = []string{token.value}
	}
	return condition, false, nil
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
