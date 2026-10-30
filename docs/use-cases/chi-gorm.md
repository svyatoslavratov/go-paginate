# net/http (chi) + GORM

A list endpoint from the handler to the repository. The handler parses the query, the repository
owns the config and the base query, and the service in between only passes the request through.

## Repository

```go
package repositories

import (
	"context"

	"gorm.io/gorm"

	"github.com/svyatoslavratov/go-paginate"
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

// ordersPage is built once; Paginate copies it per request.
var ordersPage = func() paginate.Config {
	cfg := paginate.NewConfig("orders",
		[]string{"orders.created_at", "orders.total"},
		map[string]string{
			"id":        "orders.id",
			"status":    "orders.status",
			"total":     "orders.total",
			"comment":   "orders.comment",
			"createdAt": "orders.created_at",
		})
	cfg.SearchableColumns = []string{"orders.comment"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"orders.id":         {paginate.OpIn},
		"orders.status":     {paginate.OpIn},
		"orders.total":      {paginate.OpBtw, paginate.OpGte, paginate.OpLte},
		"orders.created_at": {paginate.OpBtw},
	}
	cfg.DefaultSortBy = paginate.SortBy{"orders.created_at": paginate.Desc}
	cfg.TieBreakColumn = "orders.id"
	cfg.AllowWithDeletedInQuery = false // admins only: see below
	return *cfg
}()

// PageForUser lists the orders of one user. Scoping is the base query's job: the request's
// filters are ANDed with it and cannot widen it.
func (r *Orders) PageForUser(ctx context.Context, userID int64, req paginate.Request) (*paginate.Response[Order], error) {
	base := r.db.Model(&Order{}).Where("orders.user_id = ?", userID)
	return gormx.Paginate[Order](ctx, req, ordersPage, base)
}
```

## Handler

```go
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	req, err := paginate.ParseRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := h.orders.PageForUser(r.Context(), userID(r), *req)
	switch {
	case errors.Is(err, paginate.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, err.Error()) // safe to show
	case err != nil:
		h.log.ErrorContext(r.Context(), "list orders", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, mapPage(page)) // map models to DTOs, keep page.Meta
	}
}
```

Map rows to transport DTOs and keep the meta:

```go
func mapPage(page *paginate.Response[Order]) paginate.Response[OrderDTO] {
	data := make([]OrderDTO, 0, len(page.Data))
	for _, order := range page.Data {
		data = append(data, toDTO(order))
	}
	return paginate.Response[OrderDTO]{Data: data, Meta: page.Meta}
}
```

## Requests it serves

```http
GET /orders?filter.status=$in:paid,shipped&filter.total=$gte:1000&sortBy=total:desc
GET /orders?search=gift&page=3
GET /orders?filter.createdAt=$btw:2024-01-01,2024-02-01&paginationMode=count
```

## Preloads and soft deletes

The base query may preload relations: `r.db.Model(&Order{}).Preload("Items")`. Preloads run as
separate queries and do not change the count or the page.

GORM soft deletes apply as usual. To let some clients see deleted rows, use a second config for
them:

```go
var adminOrdersPage = func() paginate.Config {
	cfg := ordersPage
	cfg.AllowWithDeletedInQuery = true // GET /admin/orders?withDeleted=true
	return cfg
}()
```

Copying a config shares its maps and slices: change only scalar fields of a copy, or build a fresh
config.
