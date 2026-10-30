package paginate_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/internal/benchdata"
)

func BenchmarkParseRequest(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/products?"+benchdata.Query, nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := paginate.ParseRequest(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrepare(b *testing.B) {
	maxExpression := strings.Repeat("name=$eq:x OR ", 49) + "name=$eq:y"
	cases := []struct {
		name  string
		query string
	}{
		{name: "empty", query: ""},
		{name: "typical", query: benchdata.Query},
		{name: "expression", query: benchdata.ExpressionQuery},
		{name: "max-expression", query: "filter=" + url.QueryEscape(maxExpression)},
		{name: "max-in-list", query: "filter.id=$in:" + strings.TrimSuffix(strings.Repeat("7,", 1000), ",")},
	}
	cfg := benchdata.Config()
	cfg.MaxFilterValues = 1000
	for _, tc := range cases {
		values, err := url.ParseQuery(tc.query)
		if err != nil {
			b.Fatal(err)
		}
		parsed, err := paginate.ParseQuery(values, "/products")
		if err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				req := *parsed
				if _, err := paginate.Prepare(&req, cfg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type benchProduct struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Brand    string  `json:"brand"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
}

func BenchmarkNewResponse(b *testing.B) {
	rows := make([]benchProduct, 20)
	for i := range rows {
		rows[i] = benchProduct{ID: int64(i), Name: "Red lamp", Brand: "Lumen", Category: "lighting", Price: 19.99}
	}
	for _, format := range []paginate.Format{paginate.FormatDefault, paginate.FormatNestJS} {
		name := "default"
		if format == paginate.FormatNestJS {
			name = "nestjs"
		}
		cfg := benchdata.Config()
		cfg.Format = format
		r := httptest.NewRequest(http.MethodGet, "/products?"+benchdata.Query, nil)
		req, err := paginate.ParseRequest(r)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := paginate.Prepare(req, cfg); err != nil {
			b.Fatal(err)
		}
		b.Run(name+"/json", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := json.Marshal(paginate.NewResponse(rows, *req, 1234)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
