package paginate

import (
	"strings"
	"testing"
)

func BenchmarkParseFilterExpression(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "small", input: `(brand=$in:Lumen,Casa OR rating=$gte:4) AND NOT name=$sw:Old`},
		{name: "quoted", input: `name=$eq:"Milo \"the cat\"" OR name=$eq:'it\'s mine'`},
		{name: "100-nodes", input: strings.Repeat("name=$eq:x OR ", 49) + "name=$eq:y"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := parseFilterExpression(tc.input, DefaultMaxFilterExpressionComplexity); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
