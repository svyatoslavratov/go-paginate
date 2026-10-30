package paginate_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/gormx"
	"github.com/svyatoslavratov/go-paginate/internal/pgtest"
	"github.com/svyatoslavratov/go-paginate/squirrelx"
)

// The integration tests run both executors against a real Postgres (see internal/pgtest) and check
// that they agree.

func integrationConfig() paginate.Config {
	cfg := paginate.NewConfig("items", []string{"items.id", "items.created_at", "items.name"}, map[string]string{
		"id":        "items.id",
		"uid":       "items.uid",
		"name":      "items.name",
		"price":     "items.price",
		"tags":      "items.tags",
		"sizes":     "items.sizes",
		"shopId":    "items.shop_id",
		"createdAt": "items.created_at",
	})
	cfg.SearchableColumns = []string{"items.name"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"items.id":         {paginate.OpIn, paginate.OpGt},
		"items.uid":        {},
		"items.name":       paginate.AllOperators,
		"items.price":      {paginate.OpBtw},
		"items.tags":       {paginate.OpContains},
		"items.sizes":      {paginate.OpContains},
		"items.shop_id":    {},
		"items.created_at": {paginate.OpBtw, paginate.OpLt},
	}
	cfg.DefaultSortBy = paginate.SortBy{"items.id": paginate.Asc}
	cfg.TieBreakColumn = "items.id"
	return *cfg
}

// runBoth runs a request through both executors and checks they return the same ids.
func runBoth(t *testing.T, db *gorm.DB, pool *pgxpool.Pool, req paginate.Request, cfg paginate.Config) ([]int64, paginate.Meta, error) {
	t.Helper()
	ctx := context.Background()
	gormPage, gormErr := gormx.Paginate[pgtest.Item](ctx, req, cfg, db.Model(&pgtest.Item{}))
	qb := squirrel.Select("items.id").From("items").Where("items.deleted_at IS NULL")
	sqPage, sqErr := squirrelx.Paginate(ctx, req, cfg, pool, collect[int64], qb)
	if (gormErr == nil) != (sqErr == nil) {
		t.Fatalf("adapters disagree on the error: gorm %v, squirrel %v", gormErr, sqErr)
	}
	if gormErr != nil {
		if errors.Is(gormErr, paginate.ErrInvalidRequest) != errors.Is(sqErr, paginate.ErrInvalidRequest) {
			t.Fatalf("adapters disagree on the error kind: gorm %v, squirrel %v", gormErr, sqErr)
		}
		return nil, paginate.Meta{}, gormErr
	}
	gormIDs := make([]int64, 0, len(gormPage.Data))
	for _, item := range gormPage.Data {
		gormIDs = append(gormIDs, item.ID)
	}
	if !reflect.DeepEqual(gormIDs, sqPage.Data) {
		t.Fatalf("adapters disagree: gorm %v, squirrel %v", gormIDs, sqPage.Data)
	}
	if gormPage.Meta.TotalItems != sqPage.Meta.TotalItems {
		t.Fatalf("adapters disagree on the total: gorm %d, squirrel %d", gormPage.Meta.TotalItems, sqPage.Meta.TotalItems)
	}
	return gormIDs, gormPage.Meta, nil
}

func collect[T any](rows pgx.Rows) ([]T, error) {
	return pgx.CollectRows(rows, pgx.RowTo[T])
}

func TestIntegrationFilters(t *testing.T) {
	db, pool := pgtest.Open(t)
	tests := []struct {
		name    string
		req     paginate.Request
		config  func(cfg *paginate.Config)
		want    []int64
		wantErr error
	}{
		{name: "uuid compared by type", req: filters("uid", "00000000-0000-0000-0000-000000000003"), want: []int64{3}},
		{name: "bigint list", req: filters("id", "$in:1, 3"), want: []int64{1, 3}},
		{name: "numeric matches by value", req: filters("price", "$btw:10.5,20"), want: []int64{1, 2}},
		{name: "contains on text[]: every value", req: filters("tags", "$contains:red,box"), want: []int64{1, 6}},
		{name: "contains on int[]", req: filters("sizes", "$contains:2"), want: []int64{1, 2}},
		{name: "not contains", req: filters("tags", "$not:$contains:box"), want: []int64{3, 4}},
		{name: "or within a column", req: filters("id", "$in:1", "id", "$or:$gt:5"), want: []int64{1, 6}},
		{name: "or group across columns", req: filters("shopId", "$or:2", "name", "$or:$sw:big"), want: []int64{3, 4, 6}},
		{name: "date range", req: filters("createdAt", "$btw:2024-01-02,2024-01-03"), want: []int64{2, 3, 4}},
		{name: "wrong type is a validation error", req: filters("id", "$gt:abc"), wantErr: paginate.ErrInvalidRequest},
		{name: "bad uuid is a validation error", req: filters("uid", "nope"), wantErr: paginate.ErrInvalidRequest},
		{name: "compare as text keeps the old behavior", req: filters("uid", "nope"), config: func(cfg *paginate.Config) { cfg.CompareAsText = true }, want: []int64{}},
		{name: "underscore in a search is literal", req: paginate.Request{Search: "_"}, want: []int64{4}},
		{name: "percent in a search is literal", req: paginate.Request{Search: "50%"}, want: []int64{3}},
		{name: "ilike escapes wildcards", req: filters("name", "$ilike:e_b"), want: []int64{}},
		{name: "ilike matches a literal underscore", req: filters("name", "$ilike:r_s"), want: []int64{4}},
		{name: "multi-word search", req: paginate.Request{Search: "box red"}, config: func(cfg *paginate.Config) { cfg.MultiWordSearch = true }, want: []int64{1, 6}},
		{name: "whole-term search", req: paginate.Request{Search: "box red"}, want: []int64{}},
		{name: "expression: OR across fields and NOT", req: paginate.Request{FilterExpression: "(shopId=$eq:2 OR name=$sw:big) AND NOT tags=$contains:sale"}, want: []int64{4, 6}},
		{name: "expression: NOT over a group", req: paginate.Request{FilterExpression: "NOT (id=$in:1,2,3)"}, want: []int64{4, 6}},
		{name: "expression: quoted value", req: paginate.Request{FilterExpression: `name=$eq:"Red box"`}, want: []int64{1}},
		{name: "expression with filter.<field>", req: paginate.Request{FilterExpression: "id=$in:1,2 OR id=$in:6", Filter: paginate.FilterQuery{"shopId": {"1"}}}, want: []int64{1, 2, 6}},
		{name: "expression: unknown field", req: paginate.Request{FilterExpression: "secret=1"}, wantErr: paginate.ErrInvalidRequest},
		{name: "expression: wrong type", req: paginate.Request{FilterExpression: "uid=nope OR id=$in:1"}, wantErr: paginate.ErrInvalidRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := integrationConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			got, _, err := runBoth(t, db, pool, tc.req, cfg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func filters(fieldsAndValues ...string) paginate.Request {
	filter := make(paginate.FilterQuery)
	for i := 0; i+1 < len(fieldsAndValues); i += 2 {
		filter[fieldsAndValues[i]] = append(filter[fieldsAndValues[i]], fieldsAndValues[i+1])
	}
	return paginate.Request{Filter: filter}
}

func TestIntegrationPages(t *testing.T) {
	db, pool := pgtest.Open(t)
	cfg := integrationConfig()
	// Items 2 and 3 share created_at: the tie-breaker keeps them in id order on every page.
	sortByCreated := func(page uint64) paginate.Request {
		return paginate.Request{Page: page, Limit: 2, SortBy: paginate.SortBy{"createdAt": paginate.Asc}, SortKeys: []string{"createdAt"}}
	}
	var all []int64
	for page := uint64(1); page <= 3; page++ {
		ids, meta, err := runBoth(t, db, pool, sortByCreated(page), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if meta.TotalItems != 5 || meta.TotalPages != 3 || meta.CurrentPage != page {
			t.Fatalf("page %d meta: %+v", page, meta)
		}
		all = append(all, ids...)
	}
	if want := []int64{1, 2, 3, 4, 6}; !reflect.DeepEqual(all, want) {
		t.Fatalf("pages: want %v, got %v", want, all)
	}

	// Every row only when the config allows it.
	ids, meta, err := runBoth(t, db, pool, paginate.Request{RequestedMode: paginate.ModeDisabled}, cfg)
	if err != nil || len(ids) != 5 || meta.TotalPages != 1 {
		t.Fatalf("disabled pagination refused: %v %v %+v", err, ids, meta)
	}
	_ = ids
	limited := paginate.Request{RequestedMode: paginate.ModeDisabled, Limit: 2}
	if ids, _, err = runBoth(t, db, pool, limited, cfg); err != nil || len(ids) != 2 {
		t.Fatalf("without AllowDisabledPagination a client gets a page: %v %v", err, ids)
	}
	cfg.AllowDisabledPagination = true
	if ids, meta, err = runBoth(t, db, pool, limited, cfg); err != nil || len(ids) != 5 || meta.TotalPages != 1 {
		t.Fatalf("with AllowDisabledPagination: %v %v %+v", err, ids, meta)
	}
}

func TestIntegrationCountOnlyAndNestJSFormat(t *testing.T) {
	db, pool := pgtest.Open(t)
	cfg := integrationConfig()

	req, err := paginate.ParseRequest(httptest.NewRequest(http.MethodGet, "/items?limit=0&filter.shopId=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	ids, meta, err := runBoth(t, db, pool, *req, cfg)
	if err != nil || len(ids) != 0 || meta.TotalItems != 3 {
		t.Fatalf("limit=0 must count only: %v %v %+v", err, ids, meta)
	}

	cfg.Format = paginate.FormatNestJS
	req, err = paginate.ParseRequest(httptest.NewRequest(http.MethodGet, "/items?limit=2&sortBy=createdAt:desc&filter=shopId=$eq:1+OR+shopId=$eq:2", nil))
	if err != nil {
		t.Fatal(err)
	}
	page, err := gormx.Paginate[pgtest.Item](context.Background(), *req, cfg, db.Model(&pgtest.Item{}))
	if err != nil {
		t.Fatal(err)
	}
	nest, ok := page.Meta.NestJS()
	if !ok || nest.TotalItems != 5 || nest.TotalPages != 3 || !reflect.DeepEqual(nest.SortBy, [][2]string{{"createdAt", "DESC"}}) {
		t.Fatalf("nestjs meta: %+v", nest)
	}
	links := page.Meta.Links()
	if links.Next != "/items?page=2&limit=2&sortBy=createdAt:DESC&filter=shopId%3D$eq:1+OR+shopId%3D$eq:2" || links.First != "" {
		t.Fatalf("links: %+v", links)
	}
	if len(page.Data) != 2 || page.Data[0].ID != 6 {
		t.Fatalf("page: %+v", page.Data)
	}
}

func TestIntegrationGormWithDeleted(t *testing.T) {
	db, _ := pgtest.Open(t)
	cfg := integrationConfig()
	cfg.AllowWithDeletedInQuery = true
	page, err := gormx.Paginate[pgtest.Item](context.Background(), paginate.Request{WithDeleted: true}, cfg, db.Model(&pgtest.Item{}))
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.TotalItems != 6 {
		t.Fatalf("want soft-deleted rows too, got %d", page.Meta.TotalItems)
	}
}

func TestIntegrationSquirrelGroupByCount(t *testing.T) {
	_, pool := pgtest.Open(t)
	cfg := integrationConfig()
	cfg.SortableColumns = []string{"items.shop_id"}
	cfg.DefaultSortBy = paginate.SortBy{"items.shop_id": paginate.Asc}
	cfg.TieBreakColumn = ""
	qb := squirrel.Select("items.shop_id").From("items").GroupBy("items.shop_id")
	page, err := squirrelx.Paginate(context.Background(), paginate.Request{}, cfg, pool, collect[int32], qb)
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.TotalItems != 2 || !reflect.DeepEqual(page.Data, []int32{1, 2}) {
		t.Fatalf("want 2 groups, got %d %v", page.Meta.TotalItems, page.Data)
	}
}
