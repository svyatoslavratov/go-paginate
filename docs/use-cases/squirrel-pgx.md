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

var accountsPage = func() paginate.Config {
	cfg := paginate.NewConfig("accounts",
		[]string{"a.created_at", "a.balance"},
		map[string]string{
			"id":       "a.id",
			"number":   "a.number",
			"currency": "a.currency",
			"balance":  "a.balance",
			"owner":    "o.name", // a joined column
		})
	cfg.SearchableColumns = []string{"a.number", "o.name"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"a.currency": {paginate.OpIn},
		"a.balance":  {paginate.OpBtw, paginate.OpGte},
	}
	cfg.DefaultSortBy = paginate.SortBy{"a.created_at": paginate.Desc}
	cfg.TieBreakColumn = "a.id" // squirrel always appends it: leave it empty for GROUP BY or DISTINCT
	return *cfg
}()

func (r *Accounts) Page(ctx context.Context, ownerID uuid.UUID, req paginate.Request) (*paginate.Response[Account], error) {
	qb := squirrel.Select("a.id", "a.number", "a.currency", "a.balance").
		From("accounts AS a").
		Join("owners AS o ON o.id = a.owner_id").
		Where(squirrel.Eq{"a.owner_id": ownerID})
	return squirrelx.Paginate(ctx, req, accountsPage, r.pool, scanAccounts, qb)
}

func scanAccounts(rows pgx.Rows) ([]Account, error) {
	return pgx.CollectRows(rows, pgx.RowToStructByName[Account])
}
```

`squirrelx.Paginate` runs two statements: a count over the base query as a subquery, then the page.

```sql
SELECT COUNT(*) FROM (SELECT a.id, ... WHERE a.owner_id = $1 AND ...) AS paginate_count
SELECT a.id, ... WHERE a.owner_id = $1 AND ... ORDER BY a.created_at desc, a.id asc LIMIT 20 OFFSET 0
```

The subquery keeps `GROUP BY` (the count is the number of groups), `DISTINCT` and an `ORDER BY` of
the base query correct.

## OR in the base query

squirrel joins `Where` parts with a bare `AND`. Wrap OR conditions of the base query, or the filters
bind to their last part:

```go
qb = qb.Where(squirrel.Or{squirrel.Eq{"a.status": "open"}, squirrel.Eq{"a.status": "frozen"}}) // (… OR …)
qb = qb.Where("(a.status = 'open' OR a.status = 'frozen')")                                       // also fine
```

The conditions the package adds are always parenthesized.

## Seeing the SQL

`squirrelx.Statements` validates the request and returns both statements without running them:

```go
count, page, err := squirrelx.Statements(qb, &req, cfg)
log.Println(count.SQL, count.Args)
log.Println(page.SQL, page.Args)
```

## fasthttp

```go
func (c *Controller) Accounts(ctx *fasthttp.RequestCtx) {
	req, err := fasthttpx.ParseRequest(ctx)
	if err != nil {
		writeError(ctx, fasthttp.StatusBadRequest, err)
		return
	}
	page, err := c.accounts.Page(ctx, ownerID(ctx), *req)
	if errors.Is(err, paginate.ErrInvalidRequest) {
		writeError(ctx, fasthttp.StatusBadRequest, err)
		return
	}
	if err != nil {
		writeError(ctx, fasthttp.StatusInternalServerError, errors.New("internal error"))
		return
	}
	writeJSON(ctx, page)
}
```

The fasthttp parser reads the same parameters as the net/http one, with the same rules.
