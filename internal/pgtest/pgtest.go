// Package pgtest gives integration tests a disposable Postgres schema with fixture rows. Tests skip
// unless TEST_PG_DSN points to a database:
//
//	TEST_PG_DSN=postgres://postgres:test@127.0.0.1:5432/paginate?sslmode=disable go test ./...
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Fixture is the items table every test gets.
const Fixture = `
CREATE TABLE items (
	id         bigint PRIMARY KEY,
	uid        uuid NOT NULL,
	name       text NOT NULL,
	price      numeric(10, 2) NOT NULL,
	tags       text[] NOT NULL DEFAULT '{}',
	sizes      int[] NOT NULL DEFAULT '{}',
	shop_id    int NOT NULL,
	rank       int,
	created_at timestamptz NOT NULL,
	deleted_at timestamptz
);
INSERT INTO items (id, uid, name, price, tags, sizes, shop_id, rank, created_at, deleted_at) VALUES
	(1, '00000000-0000-0000-0000-000000000001', 'Red box',     10.50, '{red,box}',     '{1,2}', 1, 2,    '2024-01-01T00:00:00Z', NULL),
	(2, '00000000-0000-0000-0000-000000000002', 'Blue box',    20.00, '{blue,box}',    '{2,3}', 1, NULL, '2024-01-02T00:00:00Z', NULL),
	(3, '00000000-0000-0000-0000-000000000003', '50% off',      5.00, '{sale}',        '{1}',   2, 1,    '2024-01-02T00:00:00Z', NULL),
	(4, '00000000-0000-0000-0000-000000000004', 'Under_score',  7.25, '{}',            '{}',    2, NULL, '2024-01-03T00:00:00Z', NULL),
	(5, '00000000-0000-0000-0000-000000000005', 'Deleted red',  1.00, '{red}',         '{}',    1, 3,    '2024-01-04T00:00:00Z', '2024-02-01T00:00:00Z'),
	(6, '00000000-0000-0000-0000-000000000006', 'big red box', 30.00, '{red,big,box}', '{5}',   1, 2,    '2024-01-05T00:00:00Z', NULL);
`

// Item is a row of the fixture table for GORM. It has no json tags: cursors find its fields by
// column.
type Item struct {
	ID        int64 `gorm:"primaryKey"`
	Name      string
	ShopID    int
	Rank      *int
	CreatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// Row is a row of the fixture table for pgx.RowToStructByName.
type Row struct {
	ID        int64     `db:"id"`
	Name      string    `db:"name"`
	Rank      *int32    `db:"rank"`
	CreatedAt time.Time `db:"created_at"`
}

// RowColumns are the columns of Row.
var RowColumns = []string{"items.id", "items.name", "items.rank", "items.created_at"}

// TableName names the fixture table.
func (Item) TableName() string { return "items" }

// Open creates a schema with the fixture and returns GORM and pgx handles bound to it; the schema
// is dropped when the test ends.
func Open(t testing.TB) (*gorm.DB, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN is not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	suffix := make([]byte, 8)
	_, _ = rand.Read(suffix)
	schema := "paginate_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	// Dates without a time zone (filter.createdAt=$btw:2024-01-02,...) mean the same on any server.
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, Fixture); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDBFromPool(pool)}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db, pool
}
