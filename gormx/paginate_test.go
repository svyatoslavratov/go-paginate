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
			wantFind:  `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at desc,items.id asc LIMIT $1`,
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
			wantFind:  `SELECT * FROM "items" WHERE items.shop_id = $1 AND CAST(items.name AS TEXT) ILIKE $2 AND (((items.id IN ($3, $4)) AND ((items.name = $5) OR (items.name IS NULL)))) AND "items"."deleted_at" IS NULL ORDER BY items.created_at desc,items.id asc LIMIT $6 OFFSET $7`,
			wantVars:  []any{7, `%a\_b%`, "1", "2", "x"},
		},
		{
			name:      "contains on an array column",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{Filter: paginate.FilterQuery{"tags": {"$contains:red,big"}}},
			wantCount: `SELECT count(*) FROM "items" WHERE items.tags @> $1 AND "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE items.tags @> $1 AND "items"."deleted_at" IS NULL ORDER BY items.created_at desc,items.id asc LIMIT $2`,
			wantVars:  []any{`{"red","big"}`},
		},
		{
			name:      "withDeleted when the config allows it",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{WithDeleted: true},
			config:    func(cfg *paginate.Config) { cfg.AllowWithDeletedInQuery = true },
			wantCount: `SELECT count(*) FROM "items"`,
			wantFind:  `SELECT * FROM "items" ORDER BY items.created_at desc,items.id asc LIMIT $1`,
		},
		{
			name:      "withDeleted is ignored by default",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{WithDeleted: true},
			wantCount: `SELECT count(*) FROM "items" WHERE "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at desc,items.id asc LIMIT $1`,
		},
		{
			name:      "every row: no LIMIT",
			query:     func(db *gorm.DB) *gorm.DB { return db.Model(&gormItem{}) },
			req:       paginate.Request{Mode: paginate.ModeDisabled},
			wantCount: `SELECT count(*) FROM "items" WHERE "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT * FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at desc,items.id asc`,
		},
		{
			name: "raw DISTINCT gets no tie-breaker",
			query: func(db *gorm.DB) *gorm.DB {
				return db.Model(&gormItem{}).Select("DISTINCT items.name, items.created_at")
			},
			wantCount: `SELECT count(*) FROM "items" WHERE "items"."deleted_at" IS NULL`,
			wantFind:  `SELECT DISTINCT items.name, items.created_at FROM "items" WHERE "items"."deleted_at" IS NULL ORDER BY items.created_at desc LIMIT $1`,
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
			if _, err := Paginate[gormItem](context.Background(), tc.req, cfg, tc.query(db)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(*captured) != 2 {
				t.Fatalf("want count and find, got %+v", *captured)
			}
			count, find := (*captured)[0], (*captured)[1]
			if count.sql != tc.wantCount {
				t.Errorf("count:\nwant %s\ngot  %s", tc.wantCount, count.sql)
			}
			if find.sql != tc.wantFind {
				t.Errorf("find:\nwant %s\ngot  %s", tc.wantFind, find.sql)
			}
			if len(tc.wantVars) != 0 && !reflect.DeepEqual(count.vars, tc.wantVars) {
				t.Errorf("vars: want %#v, got %#v", tc.wantVars, count.vars)
			}
		})
	}
}

func TestPaginateGormCountOnly(t *testing.T) {
	t.Parallel()
	db, captured := dryRunDB(t)
	res, err := Paginate[gormItem](context.Background(), paginate.Request{RequestedMode: paginate.ModeCount}, itemsConfig(), db.Model(&gormItem{}))
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
	if _, err := Paginate[gormItem](context.Background(), paginate.Request{}, itemsConfig(), nil); err == nil {
		t.Fatal("want an error without a query")
	}
	req := paginate.Request{Filter: paginate.FilterQuery{"price": {"$ilike:1"}}}
	if _, err := Paginate[gormItem](context.Background(), req, itemsConfig(), db.Model(&gormItem{})); !errors.Is(err, paginate.ErrInvalidRequest) {
		t.Fatalf("want validation error, got %v", err)
	}
	if len(*captured) != 0 {
		t.Fatalf("invalid requests must not reach the database: %+v", *captured)
	}
}

func itemsConfig() paginate.Config {
	cfg := paginate.NewConfig("items", []string{"items.created_at", "items.name"}, map[string]string{
		"id":        "items.id",
		"name":      "items.name",
		"createdAt": "items.created_at",
		"tags":      "items.tags",
		"price":     "items.price",
	})
	cfg.SearchableColumns = []string{"items.name"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"items.id":    {paginate.OpIn},
		"items.name":  paginate.AllOperators,
		"items.tags":  {paginate.OpContains},
		"items.price": {paginate.OpBtw, paginate.OpGte},
	}
	cfg.DefaultSortBy = paginate.SortBy{"items.created_at": paginate.Desc}
	cfg.TieBreakColumn = "items.id"
	return *cfg
}
