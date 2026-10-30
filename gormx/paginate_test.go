package gormx

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/svyatoslavratov/go-paginate"
)

type capturedStatement struct {
	sql  string
	vars []any
}

// dryRunDB renders statements without a database and records them in order.
func dryRunDB(t *testing.T) (*gorm.DB, *[]capturedStatement) {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=dry sslmode=disable"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	captured := &[]capturedStatement{}
	err = db.Callback().Query().After("gorm:query").Register("test:capture", func(tx *gorm.DB) {
		*captured = append(*captured, capturedStatement{sql: tx.Statement.SQL.String(), vars: slices.Clone(tx.Statement.Vars)})
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, captured
}

type gormItem struct {
	ID        int64 `gorm:"primaryKey"`
	Name      string
	DeletedAt gorm.DeletedAt
}

func (gormItem) TableName() string { return "items" }

func TestPaginateGormSQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		query     func(db *gorm.DB) *gorm.DB
		req       paginate.Request
		config    func(cfg *paginate.Config)
		wantCount string
		wantFind  string
		wantVars  []any
	}{
		{
			name:      "default page",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			wantCount: `SELECT count(*) FROM "items" WHERE "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC LIMIT $1`,
		},
		{
			name:  "search and filters",
			query: func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}).Where("items.shop_id = ?", 7) },
			req: paginate.Request{
				Page:   2,
				Limit:  10,
				Search: "a_b",
				Filter: paginate.FilterQuery{"id": {"$in:1,2"}, "name": {"x", "$or:$null"}},
			},
			wantCount: `SELECT count(*) FROM "items" WHERE items.shop_id = $1 AND CAST(items.name AS TEXT) ILIKE $2 AND (((items.id IN ($3, $4)) AND ((items.name = $5) OR (items.name IS NULL)))) AND "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE items.shop_id = $1 AND CAST(items.name AS TEXT) ILIKE $2 AND (((items.id IN ($3, $4)) AND ((items.name = $5) OR (items.name IS NULL)))) AND "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC LIMIT $6 OFFSET $7`,
			wantVars:  []any{7, `%a\_b%`, "1", "2", "x"},
		},
		{
			name:      "contains on an array column",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{Filter: paginate.FilterQuery{"tags": {"$contains:red,big"}}},
			wantCount: `SELECT count(*) FROM "items" WHERE items.tags @> $1 AND "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE items.tags @> $1 AND "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC LIMIT $2`,
			wantVars:  []any{`{"red","big"}`},
		},
		{
			name:      "withDeleted when the config allows it",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{WithDeleted: true},
			config:    func(cfg *paginate.Config) { cfg.AllowWithDeletedInQuery = true },
			wantCount: `SELECT count(*) FROM "items"`,
			wantFind:  `SELECT * FROM "items" ORDER BY items.created_at DESC,items.id ASC LIMIT $1`,
		},
		{
			name:      "withDeleted is ignored by default",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{WithDeleted: true},
			wantCount: `SELECT count(*) FROM "items" WHERE "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC LIMIT $1`,
		},
		{
			name:     "every row: no LIMIT, no count",
			query:    func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:      paginate.Request{Mode: paginate.ModeDisabled},
			wantFind: `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC`,
		},
		{
			name:     "no count: a row more than the page",
			query:    func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:      paginate.Request{Limit: 5},
			config:   func(cfg *paginate.Config) { cfg.Count = paginate.CountNone },
			wantFind: `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC LIMIT $1`,
			wantVars: []any{6},
		},
		{
			name:   "cursor pagination: the keyset instead of OFFSET",
			query:  func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:    paginate.Request{Limit: 5, Cursor: itemsCursor()},
			config: func(cfg *paginate.Config) { cfg.Pagination = paginate.CursorPagination },
			// GORM parenthesizes a condition with OR once more.
			wantFind: `SELECT * FROM "items" WHERE (((items.created_at <= $1) AND ((items.created_at < $2) OR ((items.created_at = $3) AND (items.id > $4))))) AND "items"."deleted_at" IS NULL ORDER BY items.created_at DESC,items.id ASC LIMIT $5`,
			wantVars: []any{"2024-05-01T10:00:00Z", "2024-05-01T10:00:00Z", "2024-05-01T10:00:00Z", "7", 6},
		},
		{
			name: "raw DISTINCT: no tie-breaker, counted through a subquery",
			query: func(db *gorm.DB) *gorm.DB {
				return db.Model(&gormItem{}).Select("DISTINCT items.name, items.created_at")
			},
			wantCount: `SELECT count(*) FROM (SELECT DISTINCT items.name, items.created_at FROM "items" WHERE "items"."deleted_at" IS NULL) AS paginate_count`,
			wantFind:  `SELECT DISTINCT items.name, items.created_at FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at DESC LIMIT $1`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, captured := dryRunDB(t)
			cfg := itemsConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			if _, err := Paginate[gormItem](context.Background(), tc.req, paginate.MustCompile(cfg), tc.query(db)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// A count over a subquery renders the subquery first; the count and the page come last.
			statements := *captured
			if tc.wantCount != "" {
				if len(statements) < 2 || statements[len(statements)-2].sql != tc.wantCount {
					t.Fatalf("count:\nwant %s\ngot  %+v", tc.wantCount, statements)
				}
				statements = statements[len(statements)-1:]
			}
			if len(statements) != 1 || statements[0].sql != tc.wantFind {
				t.Fatalf("find:\nwant %s\ngot  %+v", tc.wantFind, statements)
			}
			if len(tc.wantVars) != 0 && !reflect.DeepEqual(statements[0].vars[:len(tc.wantVars)], tc.wantVars) {
				t.Errorf("vars: want %#v, got %#v", tc.wantVars, statements[0].vars)
			}
		})
	}
}

func TestPaginateGormCountOnly(t *testing.T) {
	t.Parallel()
	db, captured := dryRunDB(t)
	res, err := Paginate[gormItem](context.Background(), paginate.Request{RequestedMode: paginate.ModeCount}, paginate.MustCompile(itemsConfig()), db.Model(&gormItem{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*captured) != 1 || res.Data == nil || len(res.Data) != 0 {
		t.Fatalf("want the count only and empty data, got %+v and %+v", *captured, res)
	}
}

func TestPaginateGormErrors(t *testing.T) {
	t.Parallel()
	db, captured := dryRunDB(t)
	endpoint := paginate.MustCompile(itemsConfig())
	if _, err := Paginate[gormItem](context.Background(), paginate.Request{}, endpoint, nil); err == nil {
		t.Fatal("want an error without a query")
	}
	if _, err := Paginate[gormItem](context.Background(), paginate.Request{}, nil, db.Model(&gormItem{})); err == nil {
		t.Fatal("want an error without an endpoint")
	}
	req := paginate.Request{Filter: paginate.FilterQuery{"price": {"$ilike:1"}}}
	if _, err := Paginate[gormItem](context.Background(), req, endpoint, db.Model(&gormItem{})); !errors.Is(err, paginate.ErrInvalidRequest) {
		t.Fatalf("want validation error, got %v", err)
	}
	cfg := itemsConfig()
	cfg.Pagination = paginate.CursorPagination
	distinct := db.Model(&gormItem{}).Distinct("items.name")
	if _, err := Paginate[gormItem](context.Background(), paginate.Request{}, paginate.MustCompile(cfg), distinct); err == nil {
		t.Fatal("cursor pagination of a DISTINCT query without the tie-breaker must fail")
	}
	if len(*captured) != 0 {
		t.Fatalf("invalid requests must not reach the database: %+v", *captured)
	}
}

func itemsConfig() paginate.Config {
	return paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "items.id", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "name", Column: "items.name", Sortable: true, Searchable: true, Filter: paginate.AllOperators},
			{Name: "createdAt", Column: "items.created_at", Type: paginate.Time, Sortable: true},
			{Name: "tags", Column: "items.tags", Filter: []paginate.Operator{paginate.OpContains}},
			{Name: "price", Column: "items.price", Filter: []paginate.Operator{paginate.OpBtw, paginate.OpGte}},
		},
		DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
		TieBreak:    "items.id",
	}
}

// itemsCursor is a cursor of itemsConfig in cursor pagination: after the row created at
// 2024-05-01T10:00:00Z with id 7.
func itemsCursor() string {
	cfg := itemsConfig()
	cfg.Pagination = paginate.CursorPagination
	q, err := paginate.MustCompile(cfg).Prepare(&paginate.Request{Limit: 1})
	if err != nil {
		panic(err)
	}
	page, err := paginate.NewResponse(q, []map[string]any{{"id": 7, "createdAt": "2024-05-01T10:00:00Z"}, {"id": 8, "createdAt": "2024-04-30T10:00:00Z"}}, paginate.Total{})
	if err != nil {
		panic(err)
	}
	return page.Meta.NextCursor
}
