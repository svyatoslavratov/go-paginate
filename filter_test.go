package paginate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseFilters(t *testing.T) {
	t.Parallel()
	filterable := FilterableColumns{
		"t.status": {},
		"t.age":    {OpGt, OpBtw},
		"t.name":   AllOperators,
	}
	tests := []struct {
		name    string
		filter  map[string][]string
		strict  bool
		want    []columnFilter
		wantErr string
	}{
		{
			name:   "empty list allows $eq only",
			filter: map[string][]string{"t.status": {"active", "$not:$eq:banned"}},
			strict: true,
			want: []columnFilter{{column: "t.status", conditions: []filterCondition{
				{comparator: OpAnd, operator: OpEq, values: []string{"active"}},
				{comparator: OpAnd, not: true, operator: OpEq, values: []string{"banned"}},
			}}},
		},
		{
			name:    "operator outside an empty list",
			filter:  map[string][]string{"t.status": {"$ilike:act"}},
			strict:  true,
			wantErr: "operator $ilike is not allowed for status",
		},
		{
			name:   "not allowed operator is dropped without strict",
			filter: map[string][]string{"t.status": {"$ilike:act", "active"}},
			want: []columnFilter{{column: "t.status", conditions: []filterCondition{
				{comparator: OpAnd, operator: OpEq, values: []string{"active"}},
			}}},
		},
		{
			name:    "column that is not filterable",
			filter:  map[string][]string{"t.secret": {"x"}},
			strict:  true,
			wantErr: "filter on secret is not allowed",
		},
		{
			name:   "column that is not filterable is dropped without strict",
			filter: map[string][]string{"t.secret": {"x"}},
			want:   []columnFilter{},
		},
		{
			name:   "columns sorted, conditions in request order, lists trimmed",
			filter: map[string][]string{"t.name": {"$in:a, b", "$or:$null"}, "t.age": {"$btw: 4 , 6"}},
			strict: true,
			want: []columnFilter{
				{column: "t.age", conditions: []filterCondition{
					{comparator: OpAnd, operator: OpBtw, values: []string{"4", "6"}},
				}},
				{column: "t.name", conditions: []filterCondition{
					{comparator: OpAnd, operator: OpIn, values: []string{"a", "b"}},
					{comparator: OpOr, operator: OpNull},
				}},
			},
		},
		{
			name:    "malformed between",
			filter:  map[string][]string{"t.age": {"$btw:1,2,3"}},
			strict:  true,
			wantErr: "expects 1 or 2 non-empty values",
		},
		{
			name:    "list over the cap fails even without strict",
			filter:  map[string][]string{"t.name": {"$in:" + strings.Repeat("x,", 3) + "x"}},
			wantErr: "more than 3 values",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{FilterableColumns: filterable, StrictValidate: tc.strict, MaxFilterValues: 3}
			input := make(map[string]*fieldValues, len(tc.filter))
			for column, values := range tc.filter {
				input[column] = &fieldValues{field: strings.TrimPrefix(column, "t."), values: values}
			}
			got, err := parseFilters(input, cfg)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("want validation error with %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestIsAllowedOperator(t *testing.T) {
	t.Parallel()
	if !isAllowedOperator(OpEq, nil) {
		t.Fatal("$eq must always be allowed")
	}
	if isAllowedOperator(OpGt, nil) || isAllowedOperator(OpGt, []Operator{}) {
		t.Fatal("an empty list allows $eq only")
	}
	for _, operator := range AllOperators {
		if !isAllowedOperator(operator, AllOperators) {
			t.Fatalf("AllOperators must allow %s", operator)
		}
	}
}
