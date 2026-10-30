# Query syntax

`paginate.ParseRequest` (net/http) and `fasthttpx.ParseRequest` (fasthttp) read the
same parameters into a `Request`; for another framework, pass its query values to
`paginate.ParseQuery`. They fail only on a malformed `sortBy`. Everything else is checked against
the config by `paginate.Prepare`, which the executors call, so one parser serves every endpoint.

A `Request` holds what the client sent, in API field names. `Prepare` maps the fields to columns
and renders a `Query` for the executor.

## page

`page=3`: the page number, from 1. A missing, zero, negative or non-numeric page is page 1. A page
whose offset does not fit a 64-bit integer is a 400. The meta reports the requested page clamped to
`[1, totalPages]`.

## limit

`limit=50`: the page size. A missing or non-numeric limit takes `DefaultLimit` (20). A larger
limit is cut to `MaxLimit` (100).

Two values mean what they mean in nestjs-paginate:

- `limit=0` asks for the count only, the same as `paginationMode=count`: `data` is `[]`, the meta
  has the total.
- `limit=-1` asks for every row, the same as `paginationMode=disabled`, and works only where the
  config allows it.

An explicit `paginationMode` wins over both.

## sortBy

`sortBy=field:asc` or `sortBy=field:desc`, repeated. The order is case-insensitive.

```http
?sortBy=pinned:desc&sortBy=score:desc&sortBy=createdAt:asc
```

The order of the parameters is the priority of the keys: `ORDER BY pinned DESC, score DESC,
created_at ASC`. A repeated field keeps its first position and its last order. A value without
`:` or with another order is a 400 from the parser. See [sorting](sorting.md) for defaults and the
tie-breaker.

## search and searchBy

`search=ann` matches `SearchableColumns` with a case-insensitive substring match:
`CAST(column AS TEXT) ILIKE '%ann%'`, the columns ORed. `%`, `_` and `\` in the term are literal.

`searchBy=email&searchBy=name` narrows the search to some searchable fields. Fields that are not
searchable are ignored. Unknown fields are a 400 with `StrictValidate`. With
`IgnoreSearchByInQueryParam` the parameter is ignored entirely.

With `MultiWordSearch` the term is split on whitespace and every word must match some column:
`search=red box` finds "big red box" and "Red cardboard box".

The term is capped at `MaxSearchLength` characters (256).

## filter.\<field\>

`filter.age=$gte:18`, repeatable: `filter.age=$gte:18&filter.age=$lt:65`. Empty values are
ignored. The field is an API field name; dots are allowed (`filter.author.name`) and map to
whatever column the config says. See [filters](filters.md).

## filter

`filter=(color=$eq:black OR color=$eq:white) AND NOT name=$eq:Leche`: a filter expression, the
syntax of nestjs-paginate 15+, ANDed with the `filter.<field>` parameters. See
[filter expressions](filters.md#filter-expressions).

## paginationMode

- `default`: a page and the total count.
- `count`: the total count only, `data` is `[]`.
- `disabled`: every matching row and the total count. Applied only when the config sets
  `AllowDisabledPagination`; otherwise the client gets a regular page. Server code may set
  `Request.Mode` itself regardless, see [exports](use-cases/exports-and-counts.md).

## withDeleted

`withDeleted=true` includes soft-deleted rows. `gormx.Paginate` honors it when the config sets
`AllowWithDeletedInQuery`; the squirrel executor has no soft deletes and ignores it.

## Links

The parsers keep the request path in `Request.Path`. The answer builds links to the
first, previous, current, next and last pages from it, the nestjs-paginate way:

```
/cats?page=2&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3
```

The links carry `page` first, then `limit` (`0` for the count only, `-1` for every row), the
effective sort, `search`, `searchBy` when the client narrowed the search, the `filter.<field>`
parameters, `filter=` and `withDeleted`. With `FormatNestJS` they are part of the JSON;
in any format `meta.Links()` returns them, for example for a `Link` header.

Links are relative unless `LinksBaseURL` is set (`https://api.example.com`, or `/api` to restore a
prefix a proxy strips). The Host header of the request is never used.

## Building a request in code

`Request` is a plain struct. Fill it with API names, the way a parser would:

```go
req := paginate.Request{
	Limit:    50,
	SortBy:   paginate.SortBy{"createdAt": paginate.Desc},
	SortKeys: []string{"createdAt"},
	Filter:   paginate.FilterQuery{"status": {"$in:active,invited"}},
}
```

It encodes to JSON as is, for example to pass a parsed request to another service.
