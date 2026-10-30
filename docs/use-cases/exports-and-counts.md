# Exports and counts

## Totals only

`limit=0` (nestjs-paginate) or `paginationMode=count` returns the total with `data: []`, for
badges and counters. Clients may always ask for it.

```http
GET /notifications?filter.readAt=$null&limit=0
GET /notifications?filter.readAt=$null&paginationMode=count
```

## Every row, decided by the server

A CSV export or a chart over the whole filtered set needs every row. The server decides that, not
the client: keep the client's filters and search, and set the mode in code. A mode set in
`Request.Mode` is applied as is, whatever the config says.

```go
// AdminExport returns every row matching the filters of req, for a CSV file.
func (r *Orders) AdminExport(ctx context.Context, req paginate.Request) ([]Order, error) {
	req.Mode = paginate.ModeDisabled
	page, err := gormx.Paginate[Order](ctx, req, ordersPage, r.db.Model(&Order{}))
	if err != nil {
		return nil, err
	}
	return page.Data, nil
}
```

To compute over the filtered set without the client's page and sort:

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

Exports are as big as the table. Stream them or cap them in the handler.

## Every row, asked by the client

`paginationMode=disabled` and `limit=-1` (the nestjs-paginate way) work only when the endpoint's
config allows them:

```go
cfg.AllowDisabledPagination = true // small dictionaries: countries, currencies, plans
```

Without it the client gets a regular page and `meta.paginationMode` says `default`. Allow it only
for tables that stay small; a public endpoint with it lets anyone fetch the whole table in one
request.

The answer has every row on one page: `totalPages` is 1, `itemsPerPage` equals `totalItems`.

## Forcing a page

To refuse the client's mode, set it back after parsing:

```go
req, err := paginate.ParseRequest(r)
...
req.Mode = paginate.ModeDefault
```
