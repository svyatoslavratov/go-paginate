package paginate_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/gormx"
	"github.com/svyatoslavratov/go-paginate/internal/benchdata"
	"github.com/svyatoslavratov/go-paginate/internal/pgtest"
	"github.com/svyatoslavratov/go-paginate/squirrelx"
)

// BenchmarkPostgres runs benchdata.Query end to end on 10 000 products in Postgres, with the library
// and by hand, through GORM and through squirrel with pgx. It needs TEST_PG_DSN.
func BenchmarkPostgres(b *testing.B) {
	db, pool := pgtest.Open(b)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, benchdata.ProductsSQL(10_000)); err != nil {
		b.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/products?"+benchdata.Query, nil)
	cfg := benchdata.Config()
	base := squirrel.Select(benchdata.Columns...).From("products")
	scan := func(rows pgx.Rows) ([]benchdata.Product, error) {
		return pgx.CollectRows(rows, pgx.RowToStructByName[benchdata.Product])
	}

	b.Run("gorm/library", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			req, err := paginate.ParseRequest(r)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := gormx.Paginate[benchdata.Product](ctx, *req, cfg, db.Model(&benchdata.Product{})); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("gorm/handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var rows []benchdata.Product
			if _, err := benchdata.HandwrittenGORM(ctx, r.URL.Query(), db.Model(&benchdata.Product{}), &rows); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("pgx/library", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			req, err := paginate.ParseRequest(r)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := squirrelx.Paginate(ctx, *req, cfg, pool, scan, base); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("pgx/handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			countSQL, countArgs, pageSQL, pageArgs, err := benchdata.HandwrittenSelect(r.URL.Query(), base)
			if err != nil {
				b.Fatal(err)
			}
			var total int64
			if err := pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
				b.Fatal(err)
			}
			rows, err := pool.Query(ctx, pageSQL, pageArgs...)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := scan(rows); err != nil {
				b.Fatal(err)
			}
		}
	})
}
