# Query syntax

`paginate.ParseRequest` (net/http) and `fasthttpx.ParseRequest` (fasthttp) read the same
parameters into a `Request`; for another framework, pass its query values and path to
`paginate.ParseQuery`. They fail only on a malformed `sortBy`. Everything else is checked against
the endpoint by `Endpoint.Prepare`, which the executors call, so one parser serves every endpoint.

A `Request` holds what the client sent, in API field names. `Prepare` maps the fields to columns
and renders a `Query` for the executor. Its errors are 400s with a code and, when one is at fault,
the field, see [errors](errors.md).

## page

`page=3`: the page number, from 1. A page that is missing, zero, negative or not a number is
page 1. A page whose offset does not fit a 64-bit integer, a number too large for one included, is
a 400 (`invalid_page`). With a count, the meta reports the requested page clamped to
`[1, totalPages]`.

Endpoints with cursor pagination ignore `page`, see [cursor](#cursor).

## limit

`limit=50`: the page size. A missing, non-numeric or negative limit takes `DefaultLimit` (20),
apart from `-1` below. A larger limit is cut to `MaxLimit` (100).

Two values mean what they mean in nestjs-paginate:

- `limit=0` asks for the count only, the same as `paginationMode=count`: `data` is `[]`, the meta
  has the total.
- `limit=-1` asks for every row, the same as `paginationMode=disabled`, and works only where the
  config allows it.

An explicit `paginationMode` wins over both.

## sortBy

`sortBy=field:ASC` or `sortBy=field:DESC`, repeated. The order is case-insensitive.

```http
?sortBy=pinned:DESC&sortBy=score:DESC&sortBy=createdAt:ASC
```

The order of the parameters is the priority of the keys; `posts.id` is the tie-breaker:

```sql
ORDER BY posts.pinned DESC, posts.score DESC, posts.created_at ASC, posts.id ASC
```

A repeated field keeps its first position and its last order. A value without `:` or with another
order is a 400 from the parser (`invalid_sort`). A field that is unknown or not sortable is a 400
from `Prepare` (`sort_not_allowed`), or dropped with `Lenient`. See [sorting](sorting.md) for
defaults and the tie-breaker.

## search and searchBy

`search=ann` matches the searchable fields with a case-insensitive substring match:
`CAST(column AS TEXT) ILIKE $1` with `%ann%` as the argument, the fields ORed. `%`, `_` and `\` in
the term are literal.

`searchBy=email&searchBy=name` narrows the search to some searchable fields. A field that is
unknown or not searchable is a 400 (`search_not_allowed`), or dropped with `Lenient`. With
`IgnoreSearchByInQueryParam` the parameter is ignored entirely.

With `MultiWordSearch` the term is split on whitespace and every word must match some field:
`search=red box` finds "big red box" and "Red cardboard box".

The term is capped at `MaxSearchLength` characters (256); a longer one is a 400
(`search_too_long`).

## filter.\<field\>

`filter.age=$gte:18`, repeatable: `filter.age=$gte:18&filter.age=$lt:65`. Empty values are
ignored. The field is an API field name; dots are allowed (`filter.author.name`) and map to
whatever column the config says. See [filters](filters.md).

## filter

`filter=(color=$eq:black OR color=$eq:white) AND NOT name=$eq:Leche`: a filter expression, the
syntax of nestjs-paginate 15+, ANDed with the `filter.<field>` parameters. Repeated `filter=`
parameters are ANDed with each other. See [filter expressions](filters.md#filter-expressions).

## cursor

`cursor=WyJuIiwiY3JlYXRlZEF0OkRFU0MsaWQ6QVNDIiwiMjAyNC0wNS0wMlQxMDowMDowMFoiLDdd`: a page of an
endpoint with cursor pagination. The client takes it from the previous answer: `links.next` and
`links.previous` with `FormatNestJS`, `meta.nextCursor` and `meta.previousCursor` with
`FormatDefault`. It sends the cursor back with the same `sortBy`; the links carry both, and the
filters too. Without a cursor the client gets the first page.

A cursor that does not decode, was made for another sort, or holds values that do not fit the
fields is a 400 (`invalid_cursor`). Endpoints with page numbers ignore the parameter. See
[cursors](pagination.md#cursors).

## paginationMode

- `default`: a page, and the total as the endpoint counts it, see
  [counting](pagination.md#counting).
- `count`: the total count only, `data` is `[]`.
- `disabled`: every matching row on one page; the total is the number of rows. Applied only when
  the config sets `AllowDisabledPagination`; otherwise the client gets a regular page. Server code
  may set `Request.Mode` itself regardless, see [exports](use-cases/exports-and-counts.md).

An unknown mode is `default`.

## withDeleted

`withDeleted=true` includes soft-deleted rows. `gormx.Paginate` honors it when the config sets
`AllowWithDeletedInQuery`, and runs the query `Unscoped`; the squirrel executor has no soft
deletes and ignores it.

## Links

The parsers keep the request path in `Request.Path`. The answer builds links to the first,
previous, current, next and last pages from it, the nestjs-paginate way:

```
/cats?page=2&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3
```

The links carry `page` first, then `limit` (`0` for the count only, `-1` for every row), the
effective sort, `search`, `searchBy` when the client narrowed the search, the `filter.<field>`
parameters, `filter=` and `withDeleted`. With `FormatNestJS` they are part of the JSON; in any
format `meta.Links()` returns them, for example for a `Link` header.

`first` and `previous` are missing on the first page, `next` and `last` on the last one. `last`
also needs an exact count: an estimate or no count leaves it out. With cursor pagination there are
only `previous`, `current` and `next`; they have no `page` and end with the cursor:

```
/users?limit=20&sortBy=createdAt:DESC&cursor=WyJuIiwiY3JlYXRlZEF0OkRFU0MsaWQ6QVNDIiwiMjAyNC0wNS0wMlQxMDowMDowMFoiLDdd
```

Links are relative unless `LinksBaseURL` is set (`https://api.example.com`, or `/api` to restore a
prefix a proxy strips). The Host header of the request is never used.

## Building a request in code

`Request` is a plain struct. Fill it with API names, the way a parser would:

```go
req := paginate.Request{
	Limit:  50,
	SortBy: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
	Filter: paginate.FilterQuery{"status": {"$in:active,invited"}},
}
```

`Prepare` checks it like a parsed one, the sort order included. It encodes to JSON as is, for
example to pass a parsed request to another service; a sort key is a pair:

```json
{"limit":50,"sortBy":[["createdAt","DESC"]],"filter":{"status":["$in:active,invited"]}}
```
