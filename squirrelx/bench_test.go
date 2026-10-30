package squirrelx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Masterminds/squirrel"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/internal/benchdata"
	"github.com/svyatoslavratov/go-paginate/squirrelx"
)

// BenchmarkStatements compares building the count and the page statements of benchdata.Query with
// the library and by hand (benchdata.HandwrittenSelect), without a database.
func BenchmarkStatements(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/products?"+benchdata.Query, nil)
	base := squirrel.Select(benchdata.Columns...).From("products")
	cfg := benchdata.Config()

	b.Run("library", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			req, err := paginate.ParseRequest(r)
			if err != nil {
				b.Fatal(err)
			}
			if _, _, err := squirrelx.Statements(base, req, cfg); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, _, _, err := benchdata.HandwrittenSelect(r.URL.Query(), base); err != nil {
				b.Fatal(err)
			}
		}
	})
}
