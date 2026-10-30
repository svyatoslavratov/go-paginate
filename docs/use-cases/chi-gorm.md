# net/http (chi) + GORM

A list endpoint from the handler to the repository. The handler parses the query, the repository
owns the endpoint and the base query, and the service in between only passes the request through.

## Repository

```go
package repositories

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/gormx"
)

type Order struct {
	ID        int64
	UserID    int64
	Status    string
	Total     int64
	Comment   string
	CreatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// ordersConfig is the API of the list: what clients may sort, search and filter by.
func ordersConfig() paginate.Config {
	return paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "orders.id", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "status", Column: "orders.status", Type: paginate.Enum("new", "paid", "shipped", "cancelled"),
				Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "total", Column: "orders.total", Type: paginate.Int, Sortable: true,
				Filter: []paginate.Operator{paginate.OpBtw, paginate.OpGte, paginate.OpLte}},
			{Name: "comment", Column: "orders.comment", Searchable: true},
			{Name: "createdAt", Column: "orders.created_at", Type: paginate.Time, Sortable: true,
				Filter: []paginate.Operator{paginate.OpBtw}},
		},
		DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
		TieBreak:    "orders.id",
	}
}

// ordersEndpoint is compiled once, at startup: a broken config stops the service there.
var ordersEndpoint = paginate.MustCompile(ordersConfig())

type Orders struct {
	db *gorm.DB
}

// PageForUser lists the orders of one user. Scoping is the base query's job: the request's
// filters are ANDed with it and cannot widen it.
func (r *Orders) PageForUser(ctx context.Context, userID int64, req paginate.Request) (*paginate.Response[Order], error) {
	base := r.db.Model(&Order{}).Where("orders.user_id = ?", userID)
	return gormx.Paginate[Order](ctx, req, ordersEndpoint, base)
}
```

The types check values before the query runs: `filter.total=$gte:lots` is a 400 that names
`total`, and `filter.status=$in:lost` one that lists the statuses. Every field of `Config` and
`Field` is in [configuration](../configuration.md).

## Handler

```go
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	req, err := paginate.ParseRequest(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	page, err := h.orders.PageForUser(r.Context(), userID(r), *req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, paginate.Map(page, toDTO)) // DTOs, the same meta and links
}

// writeError answers the client's mistakes with a 400 and their code, field and message, anything
// else with a bare 500.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := paginate.HTTPStatus(err)
	if status == http.StatusBadRequest {
		writeJSON(w, status, err) // safe to show
		return
	}
	h.log.ErrorContext(r.Context(), "list orders", "error", err)
	writeJSON(w, status, map[string]string{"message": "internal error"})
}
```

`paginate.HTTPStatus` is 400 for the client's mistakes and 500 for the rest. The client's mistakes
are a `*paginate.Error`, whose JSON is the body of the 400:

```json
{"code":"invalid_value","field":"total","message":"invalid value \"lots\" for total: must be an integer"}
```

Encode the `*paginate.Error` itself. A layer that wraps it (`fmt.Errorf("orders: %w", err)`) keeps
the 400 but encodes as `{}`: take the error out with `errors.As` first. The codes are listed in
[errors](../errors.md).

`paginate.Map` converts the rows into transport DTOs and keeps the meta, the links included.

## Requests it serves

```http
GET /orders?filter.status=$in:paid,shipped&filter.total=$gte:1000&sortBy=total:DESC
GET /orders?search=gift&page=3
GET /orders?filter.createdAt=$btw:2024-01-01,2024-02-01&limit=0
```

The first one runs a count and the page, both scoped to the user:

```sql
SELECT count(*) FROM "orders" WHERE orders.user_id = $1 AND (((orders.status IN ($2, $3)) AND (orders.total >= $4))) AND "orders"."deleted_at" IS NULL
SELECT * FROM "orders" WHERE orders.user_id = $1 AND (((orders.status IN ($2, $3)) AND (orders.total >= $4))) AND "orders"."deleted_at" IS NULL ORDER BY orders.total DESC,orders.id ASC LIMIT $5
```

The last one runs the count alone: `limit=0` asks for the total with `data: []`. For tables too big
to count on every page, see [exports and counts](exports-and-counts.md).

## Preloads and soft deletes

The base query may preload relations: `r.db.Model(&Order{}).Preload("Items")`. Preloads run as
separate queries and do not change the count or the page.

GORM soft deletes apply as usual. To let some clients see deleted rows, give them an endpoint of
their own:

```go
// adminOrdersEndpoint also lists deleted orders: GET /admin/orders?withDeleted=true.
var adminOrdersEndpoint = paginate.MustCompile(func() paginate.Config {
	cfg := ordersConfig()
	cfg.AllowWithDeletedInQuery = true
	return cfg
}())
```

On it, `withDeleted=true` runs the query `Unscoped`; `ordersEndpoint` ignores the parameter.
`ordersConfig` builds a fresh config on every call, so the two endpoints share nothing.

## Cursors

A feed or an infinite list can page by cursor instead of page number: set
`Pagination: paginate.CursorPagination` in the config. The handler and the repository stay as they
are. The client follows `links.next` (nestjs format) or sends `meta.nextCursor` back as `?cursor=`,
with the same sort. The cursors are made of the sort fields of the rows: `Order` has them, found by
column name (`created_at` is `CreatedAt`). See [pages and counts](../pagination.md).
