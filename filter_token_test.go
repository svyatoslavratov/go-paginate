package paginate

import "testing"

func TestParseFilterToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want filterToken
	}{
		{raw: "Milo", want: filterToken{comparator: OpAnd, operator: OpEq, value: "Milo"}},
		{raw: "$gt:5", want: filterToken{comparator: OpAnd, operator: OpGt, value: "5"}},
		{raw: "$not:$in:1,2", want: filterToken{comparator: OpAnd, not: true, operator: OpIn, value: "1,2"}},
		{raw: "$or:$not:$eq:7", want: filterToken{comparator: OpOr, hasComparator: true, not: true, operator: OpEq, value: "7"}},
		{raw: "$or:7", want: filterToken{comparator: OpOr, hasComparator: true, operator: OpEq, value: "7"}},
		{raw: "$not:$or:$lt:3", want: filterToken{comparator: OpOr, hasComparator: true, not: true, operator: OpLt, value: "3"}},
		{raw: "$not:5", want: filterToken{comparator: OpAnd, not: true, operator: OpEq, value: "5"}},
		{raw: "$null", want: filterToken{comparator: OpAnd, operator: OpNull}},
		{raw: "$not:$null", want: filterToken{comparator: OpAnd, not: true, operator: OpNull}},
		{raw: "$null:x", want: filterToken{comparator: OpAnd, operator: OpNull}},
		{raw: "$eq:12:30", want: filterToken{comparator: OpAnd, operator: OpEq, value: "12:30"}},
		{raw: "$eq:$gt", want: filterToken{comparator: OpAnd, operator: OpEq, value: "$gt"}},
		{raw: "$foo:bar", want: filterToken{comparator: OpAnd, operator: OpEq, value: "$foo:bar"}},
		{raw: "$ilike:a $ilike:b", want: filterToken{comparator: OpAnd, operator: OpILike, value: "a $ilike:b"}},
		{raw: "$eq:", want: filterToken{comparator: OpAnd, operator: OpEq, value: ""}},
		{raw: "", want: filterToken{comparator: OpAnd, operator: OpEq, value: ""}},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := parseFilterToken(tc.raw); got != tc.want {
				t.Fatalf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}
