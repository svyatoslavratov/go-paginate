package paginate

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilters(t *testing.T) {
	t.Parallel()
	type parsed struct {
		field      string
		conditions []filterCondition
	}
	tests := []struct {
		name    string
		filter  FilterQuery
		lenient bool
		want    []parsed
		code    Code
		field   string
		message string
	}{
		{
			name:   "a filter of $eq only",
			filter: FilterQuery{"status": {"active", "$not:$eq:banned"}},
			want: []parsed{{"status", []filterCondition{
				{comparator: OpAnd, operator: OpEq, values: []string{"active"}},
				{comparator: OpAnd, not: true, operator: OpEq, values: []string{"banned"}},
			}}},
		},
		{
			name:    "an operator outside the list",
			filter:  FilterQuery{"status": {"$ilike:act"}},
			code:    CodeOperatorNotAllowed,
			field:   "status",
			message: "operator $ilike is not allowed for status",
		},
		{
			name:    "Lenient drops an operator that is not allowed",
			filter:  FilterQuery{"status": {"$ilike:act", "active"}},
			lenient: true,
			want: []parsed{{"status", []filterCondition{
				{comparator: OpAnd, operator: OpEq, values: []string{"active"}},
			}}},
		},
		{name: "a field that is not filterable", filter: FilterQuery{"secret": {"x"}}, code: CodeFilterNotAllowed, field: "secret", message: "filter on secret is not allowed"},
		{name: "Lenient drops a field that is not filterable", filter: FilterQuery{"secret": {"x"}}, lenient: true, want: []parsed{}},
		{
			name:   "fields sorted, conditions in request order, lists trimmed",
			filter: FilterQuery{"name": {"$in:a, b", "$or:$null"}, "age": {"$btw: 4 , 6"}},
			want: []parsed{
				{"age", []filterCondition{{comparator: OpAnd, operator: OpBtw, values: []string{"4", "6"}}}},
				{"name", []filterCondition{
					{comparator: OpAnd, operator: OpIn, values: []string{"a", "b"}},
					{comparator: OpOr, operator: OpNull},
				}},
			},
		},
		{name: "a malformed range", filter: FilterQuery{"age": {"$btw:1,2,3"}}, code: CodeInvalidValue, field: "age", message: "expects 1 or 2 non-empty values"},
		{name: "a list over the cap fails even with Lenient", filter: FilterQuery{"name": {"$in:" + strings.Repeat("x,", 3) + "x"}}, lenient: true, code: CodeTooManyValues, field: "name", message: "more than 3 values"},
		{name: "a value that is not an integer", filter: FilterQuery{"age": {"$gt:old"}}, code: CodeInvalidValue, field: "age", message: `invalid value "old" for age: must be an integer`},
		{name: "a range end that is not an integer", filter: FilterQuery{"age": {"$btw:1,x"}}, code: CodeInvalidValue, field: "age", message: `"x"`},
		{name: "an element outside the enum", filter: FilterQuery{"tags": {"$contains:new,sold"}}, code: CodeInvalidValue, field: "tags", message: "must be one of new, sale"},
		{name: "elements of the enum", filter: FilterQuery{"tags": {"$contains:new,sale"}}, want: []parsed{{"tags", []filterCondition{{comparator: OpAnd, operator: OpContains, values: []string{"new", "sale"}}}}}},
		{name: "a timestamp that does not parse", filter: FilterQuery{"seenAt": {"$gte:yesterday"}}, code: CodeInvalidValue, field: "seenAt", message: "must be a timestamp"},
		{name: "$null takes no value to check", filter: FilterQuery{"seenAt": {"$not:$null"}}, want: []parsed{{"seenAt", []filterCondition{{comparator: OpAnd, not: true, operator: OpNull}}}}},
		{name: "an empty value", filter: FilterQuery{"status": {""}}, code: CodeInvalidValue, field: "status", message: "empty filter value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := MustCompile(Config{
				Fields: []Field{
					{Name: "status", Column: "t.status", Filter: []Operator{OpEq}},
					{Name: "age", Column: "t.age", Type: Int, Filter: []Operator{OpGt, OpBtw}},
					{Name: "name", Column: "t.name", Filter: AllOperators},
					{Name: "secret", Column: "t.secret"},
					{Name: "tags", Column: "t.tags", Type: ArrayOf(Enum("new", "sale")), Filter: []Operator{OpContains}},
					{Name: "seenAt", Column: "t.seen_at", Type: Time, Filter: []Operator{OpGte, OpNull}},
				},
				TieBreak:        "t.id",
				Lenient:         tc.lenient,
				MaxFilterValues: 3,
			})
			filters, err := e.filters(&Request{Filter: tc.filter})
			if tc.code != "" {
				wantError(t, err, tc.code, tc.field, tc.message)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := make([]parsed, len(filters))
			for i, filter := range filters {
				got[i] = parsed{filter.field.Name, filter.conditions}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestIsAllowedOperator(t *testing.T) {
	t.Parallel()
	if !isAllowedOperator(OpEq, []Operator{OpGt}) {
		t.Fatal("$eq must always be allowed on a filterable field")
	}
	if isAllowedOperator(OpGt, []Operator{OpEq}) {
		t.Fatal("a list of $eq allows $eq only")
	}
	for _, operator := range AllOperators {
		if !isAllowedOperator(operator, AllOperators) {
			t.Fatalf("AllOperators must allow %s", operator)
		}
	}
}
