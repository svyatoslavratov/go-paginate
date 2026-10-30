# Comparison with nestjs-paginate

paginate follows the query language and the answers of
[nestjs-paginate](https://github.com/ppetzold/nestjs-paginate), so a frontend written for one works
with the other. This comparison was made against **nestjs-paginate 16.0.1** (October 2026).
[Migrating from nestjs-paginate](migrating-from-nestjs.md) shows every feature side by side, in
TypeScript and in Go.

Legend: ✅ the same, ⚙️ the same behind an option, ⚠️ works differently, ➕ only here, ❌ not
supported.

## Query

| Feature | nestjs-paginate 16.0.1 | paginate | Notes |
|---|---|---|---|
| `page`, `limit` | ✅ | ✅ | Defaults 20 and 100, page from 1. |
| `limit=0`: count only | ✅ | ✅ | `data: []`, the total in meta. `paginationMode=count` is the same. |
| `limit=-1`: every row | `maxLimit: -1` | ⚙️ `AllowDisabledPagination` | Without the option the client gets a regular page. |
| `sortBy=field:ASC\|DESC`, repeated | ✅ | ✅ | Here the direction is case-insensitive. |
| Malformed or unknown `sortBy` | dropped | ⚠️ 400 | Unknown fields are dropped with `StrictValidate: false`; a malformed value is always a 400. |
| `search`, `searchBy` | ✅ | ✅ | `ILIKE` over `CAST(column AS TEXT)`. |
| `%` and `_` in `search`, `$ilike`, `$sw` | wildcards | ⚠️ literal | A search for `50%` finds "50%". |
| `multiWordSearch` | ✅ | ⚙️ `MultiWordSearch` | |
| `ignoreSearchByInQueryParam` | ✅ | ✅ `IgnoreSearchByInQueryParam` | |
| `filter.<field>=` with `$eq $gt $gte $lt $lte $in $btw $null $ilike $sw $contains` | ✅ | ✅ | |
| `$not` suffix | must be allowed | ⚠️ always allowed | Allowed with every allowed operator. |
| `filterableColumns: { field: true }` | ✅ | ✅ `AllOperators` | |
| Empty operator list: `$eq` only | ✅ | ✅ | |
| Typed comparison (numbers, dates, uuids) | ✅ | ✅ | `CompareAsText` keeps the old text comparison. |
| `$contains`: array contains every value | ✅ | ✅ | `column @> '{...}'`. |
| `$btw` with one value | 400 | ➕ lower bound | |
| `$lte:YYYY-MM-DD` covers the whole day | ✅ | ❌ | Ends at midnight here; use the next day as the bound. |
| `$and`/`$or` inside `filter.<field>` | removed in 15.0 | ➕ kept | Semantics of nestjs-paginate 14. |
| `filter=<expression>` with AND, OR, NOT, parentheses, quotes | ✅ | ✅ | Same grammar, terms always validated, 100-node cap (`MaxFilterExpressionComplexity`). |
| Repeated `filter=` | ignored | ➕ ANDed | |
| `filter=` in `links` | dropped | ➕ kept | Following a link keeps the expression. |
| Empty `filter.x=` | filters by `''` | ⚠️ ignored | Inside `filter=`, `x=` filters by `''` as in nestjs. |
| `withDeleted=true` | `allowWithDeletedInQuery` | ⚙️ `AllowWithDeletedInQuery` | GORM only. |
| `select=` | ✅ | ❌ | Typed DTOs choose the columns. |
| Cursor pagination | ✅ | ❌ | Planned. |
| Relations: `relations`, to-many `EXISTS`, `$any`/`$all`/`$none` | ✅ | ❌ | The base query joins; the config maps fields to joined columns. |
| JSONB key paths, polymorphic `a~b` columns | ✅ | ⚠️ by hand | Map a field to `data->>'key'` or `COALESCE(a, b)`. |
| Caps on search length and filter values | ❌ | ➕ | `MaxSearchLength`, `MaxFilterConditions`, `MaxFilterValues`. |
| `paginationMode=default\|count\|disabled` | ❌ | ➕ | |

## Answer

| Feature | nestjs-paginate 16.0.1 | paginate | Notes |
|---|---|---|---|
| `{data, meta}` | ✅ | ✅ | |
| `meta.itemsPerPage/totalItems/currentPage/totalPages` | ✅ | ✅ | `currentPage` within `[1, totalPages]`. |
| `meta.sortBy` as `[["field", "ASC"]]`, effective sort | ✅ | ⚙️ `FormatNestJS` | The default format echoes the request: `{"field": "asc"}`. |
| `meta.filter` with a string for one value | ✅ | ⚙️ `FormatNestJS` | The default format always has arrays. |
| `meta.search`, `meta.searchBy` only with a search | ✅ | ⚙️ `FormatNestJS` | |
| `links`: first, previous, current, next, last | ✅ | ⚙️ `FormatNestJS` | `meta.Links()` gives them in any format. |
| Absolute links from the request's host | ✅ | ⚠️ `LinksBaseURL` | Links are relative unless `LinksBaseURL` is set; the Host header is never trusted. |
| `origin`, `relativePath` | ✅ | ✅ `LinksBaseURL` | |
| `meta.select`, `meta.cursor` | ✅ | ❌ | |
| `meta.paginationMode` | ❌ | ➕ default format | |

## Configuration and tooling

| Feature | nestjs-paginate 16.0.1 | paginate | Notes |
|---|---|---|---|
| `sortableColumns`, `defaultSortBy`, `nullSort` | ✅ | ✅ | |
| Default sort `[[sortableColumns[0], 'ASC']]` | ✅ | ✅ | |
| Tie-breaker on a unique column | ❌ | ➕ `TieBreakColumn` | Stable pages. |
| `defaultLimit`, `maxLimit` | ✅ | ✅ | |
| `where` | ✅ | ✅ | `Where` on the base query. |
| Query builder | ✅ | ✅ | A GORM query or a squirrel builder. |
| Global defaults (`updateGlobalConfig`) | ✅ | ⚠️ | A shared constructor of your configs. |
| `throwOnInvalidFilter` | ✅ | ⚠️ `StrictValidate` | Covers sortBy and searchBy too; on by default in `NewConfig`. |
| `buildCountQuery`, `optimizedCount` | ✅ | ⚠️ | The count wraps the query as a subquery (squirrel) or uses GORM's `Count`. |
| Swagger decorators | ✅ | ⚠️ | A hand-written [OpenAPI](use-cases/openapi.md) fragment. |
| Config check | ❌ | ➕ `Validate()` | |
| Column names from the model | entity keys | ➕ GORM builder | |
| HTTP frameworks | Express, Fastify | net/http (chi, any router), fasthttp | |
| Databases | TypeORM drivers | Postgres | SQL uses `ILIKE`, `@>` and `CAST`. |
