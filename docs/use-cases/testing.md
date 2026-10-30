# Testing a config

## Validate

Every config deserves one test line:

```go
func TestOrdersPageConfig(t *testing.T) {
	if err := ordersPage.Validate(); err != nil {
		t.Fatal(err)
	}
}
```

It catches what requests hit only one at a time: a sortable column listed by its API name instead
of its column (`"rating"` instead of `"reviews.published_rating"`), a filterable column no API field
maps to, an operator that does not exist, fields the builder could not resolve. A broken config of
this kind fails only the requests that use the broken part, or silently ignores them.

## Builder errors

The builder skips fields it cannot resolve and returns the reason from `Build()`:

```go
cfg, err := gormx.NewConfigBuilder(db, "templates", &m, &d).AddSortable(&m.Author).Build()
if err != nil { // builder: model *Template: field Author is not a column
	t.Fatal(err)
}
if err := cfg.Validate(); err != nil {
	t.Fatal(err)
}
```

## SQL snapshots

For squirrel, `squirrelx.Statements` returns the statements:

```go
req, _ := paginate.ParseRequest(httptest.NewRequest("GET", "/orders?filter.status=$in:paid,shipped&sortBy=total:desc", nil))
count, page, err := squirrelx.Statements(baseQuery(), req, cfg)
require.NoError(t, err)
require.Equal(t, `SELECT ... ORDER BY orders.total desc, orders.id asc LIMIT 20 OFFSET 0`, page.SQL)
```

For GORM, open the dialector in dry-run mode and record the statements with a callback:

```go
db, _ := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=dry"}),
	&gorm.Config{DryRun: true, DisableAutomaticPing: true})
var statements []string
_ = db.Callback().Query().After("gorm:query").Register("test:capture", func(tx *gorm.DB) {
	statements = append(statements, tx.Statement.SQL.String())
})
_, err := gormx.Paginate[Order](ctx, *req, cfg, db.Model(&Order{}))
// statements[0] is the count, statements[1] the page
```

## Against a database

The package's own integration tests run both executors against Postgres when `TEST_PG_DSN` points
to a disposable database; each test works in its own schema. The same pattern suits service
repositories: a real database catches type errors, wrong joins and sorts Postgres rejects, which
snapshots do not.
