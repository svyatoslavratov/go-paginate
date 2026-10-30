# Exports and counts

The examples use `Orders`, `ordersConfig` and `ordersEndpoint` from
[net/http (chi) + GORM](chi-gorm.md).

## Totals only

`limit=0` (nestjs-paginate) or `paginationMode=count` returns the total with `data: []`, for
badges and counters. Clients may always ask for it.

```http
GET /notifications?filter.readAt=$null&limit=0
GET /notifications?filter.readAt=$null&paginationMode=count
```

It runs the count alone, the way the endpoint counts: exactly, or as an estimate under
`CountEstimate`. Under `CountNone` it still counts exactly, since counting is all it asks for, and
endpoints with cursor pagination count it too. The answer is one page of every item: `totalItems`
is the count, `itemsPerPage` the same, `totalPages` 1.

## Every row, decided by the server

A CSV export or a chart over the whole filtered set needs every row. The server decides that, not
the client: keep the client's filters and search, and set the mode in code. A mode set in
`Request.Mode` is applied as is, whatever the config says.

```go
// AdminExport returns every order that matches the filters of req, for a CSV file.
func (r *Orders) AdminExport(ctx context.Context, req paginate.Request) ([]Order, error) {
	req.Mode = paginate.ModeDisabled
	page, err := gormx.Paginate[Order](ctx, req, ordersEndpoint, r.db.Model(&Order{}))
	if err != nil {
		return nil, err
	}
	return page.Data, nil
}
```

It runs one query, with no `LIMIT` and no count: the answer counts the rows it read. The client's
page and limit are ignored; its sort is kept. To compute over the filtered set without the
client's sort:

```go
// wholeSet keeps the filters and the search of a request, over every matching row.
func wholeSet(req paginate.Request) paginate.Request {
	return paginate.Request{
		Search:           req.Search,
		SearchBy:         req.SearchBy,
		Filter:           req.Filter,
		FilterExpression: req.FilterExpression,
		Mode:             paginate.ModeDisabled,
	}
}
```

Exports are as big as the table. Stream them, cap them in the handler, or read them page by page.

## Big exports, page by page

An endpoint with cursor pagination reads a big export page by page without `OFFSET` (with an
index on the sort and `NotNull` sort fields, a deep page costs what the first one does, see
[speed](../pagination.md#speed)), and rows inserted or deleted meanwhile do not shift the pages:

```go
// exportEndpoint pages by cursor, a thousand rows at a time, for exports that run on the server.
var exportEndpoint = paginate.MustCompile(func() paginate.Config {
	cfg := ordersConfig()
	cfg.Pagination = paginate.CursorPagination
	cfg.DefaultLimit, cfg.MaxLimit = 1000, 1000
	return cfg
}())

// ExportAll calls write with every page of the orders that match the filters and the search of req.
func (r *Orders) ExportAll(ctx context.Context, req paginate.Request, write func([]Order) error) error {
	req = paginate.Request{Search: req.Search, SearchBy: req.SearchBy, Filter: req.Filter, FilterExpression: req.FilterExpression}
	for {
		page, err := gormx.Paginate[Order](ctx, req, exportEndpoint, r.db.Model(&Order{}))
		if err != nil {
			return err
		}
		if err := write(page.Data); err != nil {
			return err
		}
		if !page.Meta.HasNextPage {
			return nil
		}
		req.Cursor = page.Meta.NextCursor
	}
}
```

Each page reads one row more than it returns (`LIMIT 1001`) to tell whether another one follows,
and counts nothing. The request keeps its sort from page to page, as a cursor requires. See
[pages and counts](../pagination.md#cursors).

## Every row, asked by the client

`paginationMode=disabled` and `limit=-1` (the nestjs-paginate way) work only when the endpoint's
config allows them:

```go
cfg.AllowDisabledPagination = true // small dictionaries: countries, currencies, plans
```

Without it the client gets a regular page and `meta.paginationMode` says `default`. Allow it only
for tables that stay small; a public endpoint with it lets anyone fetch the whole table in one
request.

The answer has every row on one page: `totalPages` is 1, `itemsPerPage` equals `totalItems`. No
count runs.

## Forcing a page

To refuse the client's mode, set it back after parsing:

```go
req, err := paginate.ParseRequest(r)
if err != nil {
	h.writeError(w, r, err)
	return
}
req.Mode = paginate.ModeDefault // a page, whatever limit and paginationMode say
```

## Counting big tables

Page numbers show a total, and `CountExact`, the default, gets it with `SELECT COUNT(*)`, which
reads every matching row. On a big table that costs more than the page. Three settings make it
cheaper; [pages and counts](../pagination.md#counting) has the details.

### No count: "load more"

```go
cfg.Count = paginate.CountNone
```

The page reads one row more than it returns (`LIMIT 21` for 20) to tell whether a next page
exists, and nothing else runs. The meta has `hasNextPage` but no totals, and the nestjs links have
no `last`:

```json
{"itemsPerPage":20,"currentPage":2,"hasNextPage":true,"hasPreviousPage":true,"sortBy":[["createdAt","DESC"]],"search":"","searchBy":[],"filter":{},"paginationMode":"default"}
```

Counters keep working: `limit=0` is counted exactly.

### An estimate, exact for small results

```go
cfg.Count = paginate.CountEstimate
cfg.ExactCountBelow = 1000
```

The total is the planner's estimate from `EXPLAIN (FORMAT JSON)` of the filtered query: one cheap
query whatever the table size, as close as the table statistics are (`ANALYZE`). An estimate below
`ExactCountBelow` is replaced by `COUNT(*)`, so a narrow filter still gets an exact number. The
meta marks an estimate:

```json
{"itemsPerPage":20,"totalItems":2500,"totalPages":125,"estimated":true,"currentPage":1,"hasNextPage":true,"hasPreviousPage":false,"sortBy":[["createdAt","DESC"]],"search":"","searchBy":[],"filter":{},"paginationMode":"default"}
```

The nestjs format has no such flag; its links drop `last`, which an estimate may place on an empty
page.

### Counting alongside the page

```go
cfg.ParallelCount = true
```

The count and the page run at the same time, on two connections, so the answer takes as long as
the slower of them. A transaction has one connection, so inside one they run one after the other:
`gormx` when its `*gorm.DB` is bound to a transaction, `squirrelx` on anything but a
`*pgxpool.Pool`.
