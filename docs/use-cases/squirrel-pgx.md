# squirrel + pgx, fasthttp

`squirrelx.Paginate` runs a squirrel `SelectBuilder` on anything with pgx's `Query` and `QueryRow`: a
`*pgxpool.Pool`, a `*pgx.Conn` or a `pgx.Tx`.

```go
type Account struct {
	ID       uuid.UUID `db:"id"`
	Number   string    `db:"number"`
	Currency string    `db:"currency"`
	Balance  int64     `db:"balance"`
}

var accountsEndpoint = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "number", Column: "a.number", Searchable: true},
		{Name: "currency", Column: "a.currency", Filter: []paginate.Operator{paginate.OpIn}},
		{Name: "balance", Column: "a.balance", Type: paginate.Int, Sortable: true,
			Filter: []paginate.Operator{paginate.OpBtw, paginate.OpGte}},
		{Name: "createdAt", Column: "a.created_at", Type: paginate.Time, Sortable: true},
		{Name: "owner", Column: "o.name", Searchable: true}, // a joined column
	},
	DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
	TieBreak:    "a.id", // squirrel always appends it: leave it empty for DISTINCT, and for GROUP BY without it
})

func (r *Accounts) Page(ctx context.Context, ownerID uuid.UUID, req paginate.Request) (*paginate.Response[Account], error) {
	qb := squirrel.Select("a.id", "a.number", "a.currency", "a.balance").
		From("accounts AS a").
		Join("owners AS o ON o.id = a.owner_id").
		Where(squirrel.Eq{"a.owner_id": ownerID})
	return squirrelx.Paginate(ctx, req, accountsEndpoint, r.pool, scanAccounts, qb)
}

func scanAccounts(rows pgx.Rows) ([]Account, error) {
	return pgx.CollectRows(rows, pgx.RowToStructByName[Account])
}
```

`squirrelx.Paginate` runs two statements: a count over the base query as a subquery, then the page.
For `GET /accounts?search=ann&filter.currency=$in:EUR,USD&sortBy=balance:DESC`:

```sql
SELECT COUNT(*) FROM (SELECT a.id, a.number, a.currency, a.balance FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id WHERE a.owner_id = $1 AND ((CAST(a.number AS TEXT) ILIKE $2) OR (CAST(o.name AS TEXT) ILIKE $3)) AND a.currency IN ($4, $5)) AS paginate_count
SELECT a.id, a.number, a.currency, a.balance FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id WHERE a.owner_id = $1 AND ((CAST(a.number AS TEXT) ILIKE $2) OR (CAST(o.name AS TEXT) ILIKE $3)) AND a.currency IN ($4, $5) ORDER BY a.balance DESC, a.id ASC LIMIT 20 OFFSET 0
```

The subquery keeps `GROUP BY` (the count is the number of groups), `DISTINCT` and an `ORDER BY` of
the base query correct. With `ParallelCount`, the two run at the same time on a `*pgxpool.Pool`,
and one after the other on a `*pgx.Conn` or a `pgx.Tx`, which have one connection. For counts
that do not read every row, see [exports and counts](exports-and-counts.md).

## OR in the base query

squirrel joins `Where` parts with a bare `AND`. Wrap OR conditions of the base query, or the filters
bind to their last part:

```go
qb = qb.Where(squirrel.Or{squirrel.Eq{"a.status": "open"}, squirrel.Eq{"a.status": "frozen"}}) // (… OR …)
qb = qb.Where("(a.status = 'open' OR a.status = 'frozen')")                                    // also fine
```

The conditions the package adds are safe to AND: an OR in them is always parenthesized.

## Seeing the SQL

`squirrelx.Statements` checks the request and returns what `Paginate` would run, without running
it:

```go
plan, err := squirrelx.Statements(qb, &req, accountsEndpoint)
if err != nil {
	return err // the client's mistakes, as Paginate reports them
}
log.Println(plan.Count.SQL, plan.Count.Args)
log.Println(plan.Page.SQL, plan.Page.Args)
```

A statement the request does not need is empty: `Page` for the count only (`limit=0`), `Count`
under `CountNone`, in cursor pagination and for every row. Under `CountEstimate`, `Estimate` is the
`EXPLAIN (FORMAT JSON)` of the filtered query, which runs first, and `Count` runs only for an
estimate below `ExactCountBelow`. `Statements` normalizes `req` in place, as `Endpoint.Prepare`
does.

## Cursors

In cursor pagination (`Pagination: paginate.CursorPagination`) the cursors are made of the sort
values of the first and the last row, so the rows must carry the sort fields. `Account` above
lacks `created_at`, the default sort, and such an endpoint fails after the query with
`… has no field for the sort key createdAt`. Select `a.created_at` too and give `Account` a field
for it:

```go
type Account struct {
	ID        uuid.UUID `db:"id"`
	Number    string    `db:"number"`
	Currency  string    `db:"currency"`
	Balance   int64     `db:"balance"`
	CreatedAt time.Time `db:"created_at"`
}
```

The tie-breaker `a.id` is found by its column name, the `db:"id"` tag. See
[pages and counts](../pagination.md).

## fasthttp

```go
func (c *Controller) Accounts(ctx *fasthttp.RequestCtx) {
	req, err := fasthttpx.ParseRequest(ctx)
	if err != nil {
		writeError(ctx, err)
		return
	}
	page, err := c.accounts.Page(ctx, ownerID(ctx), *req)
	if err != nil {
		writeError(ctx, err)
		return
	}
	writeJSON(ctx, fasthttp.StatusOK, page)
}

// writeError answers the client's mistakes with a 400 and their code, field and message, anything
// else with a bare 500. HTTPStatus returns the numbers fasthttp uses.
func writeError(ctx *fasthttp.RequestCtx, err error) {
	status := paginate.HTTPStatus(err)
	if status == fasthttp.StatusBadRequest {
		writeJSON(ctx, status, err)
		return
	}
	log.Printf("list accounts: %v", err)
	writeJSON(ctx, status, map[string]string{"message": "internal error"})
}
```

The fasthttp parser reads the same parameters as the net/http one, with the same rules. See
[errors](../errors.md) for the body of a 400.
