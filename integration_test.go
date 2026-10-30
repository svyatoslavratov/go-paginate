package paginate_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
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
	return paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "items.id", Type: paginate.Int, Sortable: true, Filter: []paginate.Operator{paginate.OpIn, paginate.OpGt}},
			// uid has no Type: Postgres checks its values.
			{Name: "uid", Column: "items.uid", Filter: []paginate.Operator{paginate.OpEq}},
			{Name: "name", Column: "items.name", Sortable: true, Searchable: true, Filter: paginate.AllOperators},
			{Name: "price", Column: "items.price", Type: paginate.Number, Filter: []paginate.Operator{paginate.OpBtw}},
			{Name: "tags", Column: "items.tags", Type: paginate.ArrayOf(paginate.Text), Filter: []paginate.Operator{paginate.OpContains}},
			{Name: "sizes", Column: "items.sizes", Type: paginate.ArrayOf(paginate.Int), Filter: []paginate.Operator{paginate.OpContains}},
			{Name: "shopId", Column: "items.shop_id", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpEq}},
			{Name: "rank", Column: "items.rank", Type: paginate.Int, Sortable: true, Filter: []paginate.Operator{paginate.OpNull}},
			{Name: "createdAt", Column: "items.created_at", Type: paginate.Time, Sortable: true, Filter: []paginate.Operator{paginate.OpBtw, paginate.OpLt}},
		},
		DefaultSort: []paginate.Sort{{Field: "id", Order: paginate.Asc}},
		TieBreak:    "items.id",
	}
}

// runBoth runs a request through both executors and checks they return the same rows and meta.
func runBoth(t *testing.T, db *gorm.DB, pool *pgxpool.Pool, req paginate.Request, cfg paginate.Config) ([]int64, paginate.Meta, error) {
	t.Helper()
	ctx := context.Background()
	endpoint, err := paginate.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	gormPage, gormErr := gormx.Paginate[pgtest.Item](ctx, req, endpoint, db.Model(&pgtest.Item{}))
	qb := squirrel.Select(pgtest.RowColumns...).From("items").Where("items.deleted_at IS NULL")
	sqPage, sqErr := squirrelx.Paginate(ctx, req, endpoint, pool, collect[pgtest.Row], qb)
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
	sqIDs := make([]int64, 0, len(sqPage.Data))
	for _, row := range sqPage.Data {
		sqIDs = append(sqIDs, row.ID)
	}
	if !reflect.DeepEqual(gormIDs, sqIDs) {
		t.Fatalf("adapters disagree: gorm %v, squirrel %v", gormIDs, sqIDs)
	}
	gm, sm := gormPage.Meta, sqPage.Meta
	if gm.TotalItems != sm.TotalItems || gm.Count != sm.Count || gm.HasNextPage != sm.HasNextPage ||
		gm.HasPreviousPage != sm.HasPreviousPage || gm.NextCursor != sm.NextCursor || gm.PreviousCursor != sm.PreviousCursor {
		t.Fatalf("adapters disagree on the meta:\ngorm     %+v\nsquirrel %+v", gm, sm)
	}
	return gormIDs, gm, nil
}

func collect[T any](rows pgx.Rows) ([]T, error) {
	return pgx.CollectRows(rows, pgx.RowToStructByName[T])
}

func TestIntegrationFilters(t *testing.T) {
	db, pool := pgtest.Open(t)
	tests := []struct {
		name     string
		req      paginate.Request
		config   func(cfg *paginate.Config)
		want     []int64
		wantCode paginate.Code
	}{
		{name: "uuid compared by type", req: filters("uid", "00000000-0000-0000-0000-000000000003"), want: []int64{3}},
		{name: "bigint list", req: filters("id", "$in:1, 3"), want: []int64{1, 3}},
		{name: "numeric matches by value", req: filters("price", "$btw:10.5,20"), want: []int64{1, 2}},
		{name: "contains on text[]: every value", req: filters("tags", "$contains:red,box"), want: []int64{1, 6}},
		{name: "contains on int[]", req: filters("sizes", "$contains:2"), want: []int64{1, 2}},
		{name: "not contains", req: filters("tags", "$not:$contains:box"), want: []int64{3, 4}},
		{name: "or within a field", req: filters("id", "$in:1", "id", "$or:$gt:5"), want: []int64{1, 6}},
		{name: "or group across fields", req: filters("shopId", "$or:2", "name", "$or:$sw:big"), want: []int64{3, 4, 6}},
		{name: "date range", req: filters("createdAt", "$btw:2024-01-02,2024-01-03"), want: []int64{2, 3, 4}},
		{name: "null", req: filters("rank", "$null"), want: []int64{2, 4}},
		{name: "a typed field rejects a value before the query", req: filters("id", "$gt:abc"), wantCode: paginate.CodeInvalidValue},
		{name: "Postgres rejects a value of an untyped field", req: filters("uid", "nope"), wantCode: paginate.CodeInvalidValue},
		{
			name:     "a UUID field rejects it before the query",
			req:      filters("uid", "nope"),
			config:   func(cfg *paginate.Config) { cfg.Fields[1].Type = paginate.UUID },
			wantCode: paginate.CodeInvalidValue,
		},
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
		{name: "expression: unknown field", req: paginate.Request{FilterExpression: "secret=1"}, wantCode: paginate.CodeUnknownField},
		{name: "expression: wrong type", req: paginate.Request{FilterExpression: "uid=nope OR id=$in:1"}, wantCode: paginate.CodeInvalidValue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := integrationConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			got, _, err := runBoth(t, db, pool, tc.req, cfg)
			if tc.wantCode != "" {
				var pe *paginate.Error
				if !errors.As(err, &pe) || pe.Code != tc.wantCode || paginate.HTTPStatus(err) != http.StatusBadRequest {
					t.Fatalf("want %s, got %v", tc.wantCode, err)
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
		return paginate.Request{Page: page, Limit: 2, SortBy: []paginate.Sort{{Field: "createdAt", Order: paginate.Asc}}}
	}
	var all []int64
	for page := uint64(1); page <= 3; page++ {
		ids, meta, err := runBoth(t, db, pool, sortByCreated(page), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if meta.TotalItems != 5 || meta.TotalPages != 3 || meta.CurrentPage != page || meta.HasNextPage != (page < 3) {
			t.Fatalf("page %d meta: %+v", page, meta)
		}
		all = append(all, ids...)
	}
	if want := []int64{1, 2, 3, 4, 6}; !reflect.DeepEqual(all, want) {
		t.Fatalf("pages: want %v, got %v", want, all)
	}

	// Every row only when the config allows it.
	limited := paginate.Request{RequestedMode: paginate.ModeDisabled, Limit: 2}
	if ids, _, err := runBoth(t, db, pool, limited, cfg); err != nil || len(ids) != 2 {
		t.Fatalf("without AllowDisabledPagination a client gets a page: %v %v", err, ids)
	}
	cfg.AllowDisabledPagination = true
	if ids, meta, err := runBoth(t, db, pool, limited, cfg); err != nil || len(ids) != 5 || meta.TotalItems != 5 || meta.TotalPages != 1 {
		t.Fatalf("with AllowDisabledPagination: %v %v %+v", err, ids, meta)
	}
}

// TestIntegrationCursor walks every page forward and back in cursor pagination and checks the walk
// reads the rows in the order of the same sort without pagination.
func TestIntegrationCursor(t *testing.T) {
	db, pool := pgtest.Open(t)
	sorts := []struct {
		name     string
		sortBy   []paginate.Sort
		nullSort paginate.NullSort
	}{
		{name: "rank, NULL values where Postgres puts them", sortBy: []paginate.Sort{{Field: "rank", Order: paginate.Asc}}},
		{name: "rank descending, NULL values where Postgres puts them", sortBy: []paginate.Sort{{Field: "rank", Order: paginate.Desc}}},
		{name: "rank, NULL values first", sortBy: []paginate.Sort{{Field: "rank", Order: paginate.Asc}}, nullSort: paginate.NullsFirst},
		{name: "rank descending, NULL values last", sortBy: []paginate.Sort{{Field: "rank", Order: paginate.Desc}}, nullSort: paginate.NullsLast},
		{name: "two keys", sortBy: []paginate.Sort{{Field: "rank", Order: paginate.Desc}, {Field: "createdAt", Order: paginate.Asc}}, nullSort: paginate.NullsLast},
		{name: "equal timestamps", sortBy: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}}},
		{name: "text", sortBy: []paginate.Sort{{Field: "name", Order: paginate.Asc}}},
		{name: "the default sort", sortBy: nil},
	}
	for _, sort := range sorts {
		t.Run(sort.name, func(t *testing.T) {
			cfg := integrationConfig()
			cfg.NullSort = sort.nullSort
			want, _, err := runBoth(t, db, pool, paginate.Request{SortBy: sort.sortBy, Mode: paginate.ModeDisabled}, cfg)
			if err != nil {
				t.Fatal(err)
			}

			cfg.Pagination = paginate.CursorPagination
			var forward []int64
			var last paginate.Meta
			var lastIDs []int64
			for cursor, pages := "", 0; ; pages++ {
				if pages > len(want) {
					t.Fatalf("the walk does not end: %v", forward)
				}
				ids, meta, err := runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: sort.sortBy, Cursor: cursor}, cfg)
				if err != nil {
					t.Fatal(err)
				}
				if meta.HasPreviousPage != (cursor != "") || meta.TotalItems != 0 || meta.Count != paginate.CountNone {
					t.Fatalf("page %d meta: %+v", pages, meta)
				}
				forward = append(forward, ids...)
				last, lastIDs = meta, ids
				if !meta.HasNextPage {
					break
				}
				cursor = meta.NextCursor
			}
			if !reflect.DeepEqual(forward, want) {
				t.Fatalf("forward: want %v, got %v", want, forward)
			}

			backward := slices.Clone(lastIDs)
			for meta := last; meta.HasPreviousPage; {
				var ids []int64
				ids, meta, err = runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: sort.sortBy, Cursor: meta.PreviousCursor}, cfg)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.HasNextPage {
					t.Fatalf("a page reached backward must have a next page: %+v", meta)
				}
				backward = append(ids, backward...)
			}
			if !reflect.DeepEqual(backward, want) {
				t.Fatalf("backward: want %v, got %v", want, backward)
			}
		})
	}
}

func TestIntegrationCursorEdges(t *testing.T) {
	db, pool := pgtest.Open(t)
	cfg := integrationConfig()
	cfg.Pagination = paginate.CursorPagination
	sortBy := []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}}

	first, meta, err := runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: sortBy}, cfg)
	if err != nil || !reflect.DeepEqual(first, []int64{6, 4}) {
		t.Fatalf("first page: %v %v", first, err)
	}
	next := meta.NextCursor

	_, _, err = runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: []paginate.Sort{{Field: "rank", Order: paginate.Asc}}, Cursor: next}, cfg)
	if pe := (*paginate.Error)(nil); !errors.As(err, &pe) || pe.Code != paginate.CodeInvalidCursor {
		t.Fatalf("a cursor of another sort must be refused, got %v", err)
	}
	if _, _, err = runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: sortBy, Cursor: "garbage"}, cfg); paginate.HTTPStatus(err) != http.StatusBadRequest {
		t.Fatalf("a broken cursor must be a 400, got %v", err)
	}

	// The rows after the first page are gone: the next page is empty, and its way back leads to
	// the first page again, the row of the cursor included.
	if _, err := pool.Exec(context.Background(), "UPDATE items SET deleted_at = now() WHERE id IN (1, 2, 3)"); err != nil {
		t.Fatal(err)
	}
	empty, meta, err := runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: sortBy, Cursor: next}, cfg)
	if err != nil || len(empty) != 0 || meta.HasNextPage || !meta.HasPreviousPage {
		t.Fatalf("empty page: %v %+v %v", empty, meta, err)
	}
	back, _, err := runBoth(t, db, pool, paginate.Request{Limit: 2, SortBy: sortBy, Cursor: meta.PreviousCursor}, cfg)
	if err != nil || !reflect.DeepEqual(back, []int64{6, 4}) {
		t.Fatalf("the way back from an empty page: %v %v", back, err)
	}
}

func TestIntegrationCountModes(t *testing.T) {
	db, pool := pgtest.Open(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "ANALYZE items"); err != nil {
		t.Fatal(err)
	}

	cfg := integrationConfig()
	cfg.Count = paginate.CountNone
	ids, meta, err := runBoth(t, db, pool, paginate.Request{Limit: 2}, cfg)
	if err != nil || len(ids) != 2 || !meta.HasNextPage || meta.TotalItems != 0 || meta.Count != paginate.CountNone {
		t.Fatalf("no count, first page: %v %+v %v", ids, meta, err)
	}
	ids, meta, err = runBoth(t, db, pool, paginate.Request{Limit: 2, Page: 3}, cfg)
	if err != nil || !reflect.DeepEqual(ids, []int64{6}) || meta.HasNextPage {
		t.Fatalf("no count, last page: %v %+v %v", ids, meta, err)
	}
	ids, meta, err = runBoth(t, db, pool, paginate.Request{RequestedMode: paginate.ModeCount}, cfg)
	if err != nil || len(ids) != 0 || meta.TotalItems != 5 || meta.Count != paginate.CountExact {
		t.Fatalf("no count, count only: %v %+v %v", ids, meta, err)
	}

	cfg.Count = paginate.CountEstimate
	if _, meta, err = runBoth(t, db, pool, paginate.Request{Limit: 2}, cfg); err != nil || meta.Count != paginate.CountEstimate || meta.TotalItems == 0 {
		t.Fatalf("estimate: %+v %v", meta, err)
	}
	filtered := paginate.Request{Limit: 2, Filter: paginate.FilterQuery{"shopId": {"1"}, "name": {"$ilike:box"}}}
	if _, meta, err = runBoth(t, db, pool, filtered, cfg); err != nil || meta.Count != paginate.CountEstimate {
		t.Fatalf("an estimate with filters, which EXPLAIN takes as parameters: %+v %v", meta, err)
	}
	cfg.ExactCountBelow = 1000
	if _, meta, err = runBoth(t, db, pool, paginate.Request{Limit: 2}, cfg); err != nil || meta.Count != paginate.CountExact || meta.TotalItems != 5 {
		t.Fatalf("estimate below the threshold: %+v %v", meta, err)
	}

	cfg.Count, cfg.ParallelCount = paginate.CountExact, true
	if ids, meta, err = runBoth(t, db, pool, paginate.Request{Limit: 2, Filter: paginate.FilterQuery{"shopId": {"1"}}}, cfg); err != nil || !reflect.DeepEqual(ids, []int64{1, 2}) || meta.TotalItems != 3 {
		t.Fatalf("parallel count: %v %+v %v", ids, meta, err)
	}
	endpoint := paginate.MustCompile(cfg)
	err = db.Transaction(func(tx *gorm.DB) error {
		page, err := gormx.Paginate[pgtest.Item](ctx, paginate.Request{Limit: 2}, endpoint, tx.Model(&pgtest.Item{}))
		if err == nil && page.Meta.TotalItems != 5 {
			t.Errorf("in a transaction: %+v", page.Meta)
		}
		return err
	})
	if err != nil {
		t.Fatalf("a transaction runs the count and the page one after the other: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qb := squirrel.Select(pgtest.RowColumns...).From("items").Where("items.deleted_at IS NULL")
	if page, err := squirrelx.Paginate(ctx, paginate.Request{Limit: 2}, endpoint, tx, collect[pgtest.Row], qb); err != nil || page.Meta.TotalItems != 5 {
		t.Fatalf("squirrel in a transaction: %v", err)
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
	page, err := gormx.Paginate[pgtest.Item](context.Background(), *req, paginate.MustCompile(cfg), db.Model(&pgtest.Item{}))
	if err != nil {
		t.Fatal(err)
	}
	nest, ok := page.Meta.NestJS()
	if !ok || *nest.TotalItems != 5 || *nest.TotalPages != 3 || !reflect.DeepEqual(nest.SortBy, []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}}) {
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
	page, err := gormx.Paginate[pgtest.Item](context.Background(), paginate.Request{WithDeleted: true}, paginate.MustCompile(cfg), db.Model(&pgtest.Item{}))
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.TotalItems != 6 {
		t.Fatalf("want soft-deleted rows too, got %d", page.Meta.TotalItems)
	}
}

func TestIntegrationSquirrelGroupByCount(t *testing.T) {
	_, pool := pgtest.Open(t)
	endpoint := paginate.MustCompile(paginate.Config{
		Fields:      []paginate.Field{{Name: "shopId", Column: "items.shop_id", Sortable: true}},
		DefaultSort: []paginate.Sort{{Field: "shopId", Order: paginate.Asc}},
	})
	qb := squirrel.Select("items.shop_id").From("items").GroupBy("items.shop_id")
	page, err := squirrelx.Paginate(context.Background(), paginate.Request{}, endpoint, pool, func(rows pgx.Rows) ([]int32, error) {
		return pgx.CollectRows(rows, pgx.RowTo[int32])
	}, qb)
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.TotalItems != 2 || !reflect.DeepEqual(page.Data, []int32{1, 2}) {
		t.Fatalf("want 2 groups, got %d %v", page.Meta.TotalItems, page.Data)
	}
}
