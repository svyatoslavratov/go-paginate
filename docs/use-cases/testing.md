# Testing a config

## Compile

Every endpoint deserves one test line:

```go
func TestOrdersEndpoint(t *testing.T) {
	if _, err := paginate.Compile(ordersConfig()); err != nil {
		t.Fatal(err)
	}
}
```

`Compile` is the check: no endpoint runs without it, and the test makes a broken config fail in CI
instead of at startup. It catches a field that lost its name or its column, a name used twice, an
operator that does not exist or does not fit the field's type, an enum without values, a default
sort on a field that was renamed, cursor pagination without a tie-breaker. It lists every problem
at once:

```text
paginate: invalid config: field rating: $contains needs an array type, the field is int
default sort: unknown field createdAt
no sort: set DefaultSort, make a field sortable, or set TieBreak
```

An endpoint declared with `MustCompile` as a package variable is checked by any test of its
package: a broken config panics when the package loads.

```go
var ordersEndpoint = paginate.MustCompile(ordersConfig())
```

## Builder errors

`ConfigOf` and the GORM builder report what they cannot read. `ConfigOf` names every bad tag:

```go
type Product struct {
	ID    int64   `json:"id" paginate:"unique"`
	Price float64 `json:"price" paginate:"sortable;filter:$gte"` // sort, not sortable
	Name  string  `json:"name" paginate:"sort;type:txt"`         // text, not txt
}

_, err := paginate.ConfigOf[Product]("products")
```

```text
paginate: invalid config: Price: unknown paginate setting "sortable"
Name: unknown type "txt"
```

The builder leaves out the fields it cannot resolve and returns the reasons from `Config()`:

```go
b := gormx.NewBuilder(db, "templates", &m, &d)
b.Field(&d.Title, &m.Title).Sortable().Searchable()
b.Field(&d.Author, &m.Author).Sortable() // a relation, not a column
cfg, err := b.Config()
if err != nil { // builder: model *repo.Template: field Author is not a column
	t.Fatal(err)
}
if _, err := paginate.Compile(cfg); err != nil {
	t.Fatal(err)
}
```

Neither needs a database: the builder's `db` only lends it the naming strategy.

## Requests

`Prepare` needs no database either, so a test can pin down what a client gets for a request:

```go
func TestOrdersRefuseBadTotals(t *testing.T) {
	req := paginate.Request{Filter: paginate.FilterQuery{"total": {"$gte:lots"}}}
	_, err := ordersEndpoint.Prepare(&req)
	var pe *paginate.Error
	if !errors.As(err, &pe) || pe.Code != paginate.CodeInvalidValue || pe.Field != "total" {
		t.Fatalf("want invalid_value for total, got %v", err)
	}
}
```

In cursor pagination the rows must carry the sort fields, or `NewResponse` cannot make the
cursors. It reads them only when there is a page to link to, so test with one row more than the
limit:

```go
q, err := feedEndpoint.Prepare(&paginate.Request{Limit: 1})
if err != nil {
	t.Fatal(err)
}
if _, err := paginate.NewResponse(q, []OrderDTO{{ID: 1}, {ID: 2}}, paginate.Total{}); err != nil {
	t.Fatal(err) // paginate: cursor pagination: repo.OrderDTO has no field for the sort key createdAt
}
```

## SQL snapshots

For squirrel, `squirrelx.Statements` returns the statements `Paginate` runs:

```go
req, _ := paginate.ParseRequest(httptest.NewRequest("GET", "/orders?filter.status=$in:paid,shipped&sortBy=total:desc", nil))
qb := squirrel.Select("orders.id", "orders.status", "orders.total").From("orders").Where(squirrel.Eq{"orders.shop_id": 7})
plan, err := squirrelx.Statements(qb, req, ordersEndpoint)
if err != nil {
	t.Fatal(err)
}
const want = `SELECT orders.id, orders.status, orders.total FROM orders WHERE orders.shop_id = $1 AND orders.status IN ($2, $3) ORDER BY orders.total DESC, orders.id ASC LIMIT 20 OFFSET 0`
if plan.Page.SQL != want {
	t.Fatalf("page:\nwant %s\ngot  %s", want, plan.Page.SQL)
}
```

`plan.Count` is the count and `plan.Estimate` the `EXPLAIN` of `CountEstimate`; a statement the
request does not need is empty.

For GORM, open the dialector in dry-run mode and record the statements with a callback:

```go
db, _ := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=dry"}),
	&gorm.Config{DryRun: true, DisableAutomaticPing: true})
var statements []string
_ = db.Callback().Query().After("gorm:query").Register("test:capture", func(tx *gorm.DB) {
	statements = append(statements, tx.Statement.SQL.String())
})
_, err := gormx.Paginate[Order](ctx, *req, ordersEndpoint, db.Model(&Order{}))
// statements[0] is the count, statements[1] the page:
// SELECT count(*) FROM "orders" WHERE orders.status IN ($1, $2)
// SELECT * FROM "orders" WHERE orders.status IN ($1, $2) ORDER BY orders.total DESC,orders.id ASC LIMIT $3
```

Without a count (`CountNone`, cursor pages) the page is the only statement. `CountEstimate` sends
its `EXPLAIN` to the connection itself, so a dry run fails there: snapshot such an endpoint with
`CountExact`, or test it against a database.

## Against a database

The package's own integration tests run both executors against Postgres when `TEST_PG_DSN` points
to a disposable database; each test works in its own schema. The same pattern suits service
repositories: a real database catches type errors, wrong joins and sorts Postgres rejects, which
snapshots do not.
