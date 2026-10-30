package gormx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/gormx"
	"github.com/svyatoslavratov/go-paginate/internal/benchdata"
)

// BenchmarkDryRun compares running benchdata.Query through GORM with the library and by hand
// (benchdata.HandwrittenGORM), with GORM in dry-run mode: both build the same count and select,
// nothing reaches a database.
func BenchmarkDryRun(b *testing.B) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=dry sslmode=disable"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		b.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/products?"+benchdata.Query, nil)
	endpoint := benchdata.Endpoint()
	ctx := context.Background()

	b.Run("library", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			req, err := paginate.ParseRequest(r)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := gormx.Paginate[benchdata.Product](ctx, *req, endpoint, db.Model(&benchdata.Product{})); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var rows []benchdata.Product
			if _, err := benchdata.HandwrittenGORM(ctx, r.URL.Query(), db.Model(&benchdata.Product{}), &rows); err != nil {
				b.Fatal(err)
			}
		}
	})
}
