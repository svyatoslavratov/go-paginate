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
| `limit=-1`: every row | `maxLimit: -1` | ⚙️ `AllowDisabledPagination` | Without the option the client gets a regular page. `maxLimit: -1` also lifts the cap on `limit`; here `MaxLimit` still applies. |
| `sortBy=field:ASC\|DESC`, repeated | ✅ | ✅ | Here the direction is case-insensitive. |
| Unknown `sortBy` and `searchBy` fields | dropped | ⚠️ 400 | Dropped with `Lenient`. A malformed `sortBy` is a 400 either way. |
| Unknown filter fields, operators that are not allowed | dropped; 400 with `throwOnInvalidFilter` | ⚠️ 400 | Dropped with `Lenient`. |
| A value that does not fit the column (`$gte:old` on a number) | 500 from the database | ➕ 400 | `Field.Type` (`Int`, `Number`, `Bool`, `Time`, `Date`, `UUID`, `Enum`, `ArrayOf`) checks values before the query. Without a `Type`, the value Postgres rejects is a 400 too. |
| `search`, `searchBy` | ✅ | ✅ | `ILIKE` over `CAST(column AS TEXT)`. |
| `%` and `_` in `search`, `$ilike`, `$sw` | wildcards | ⚠️ literal | A search for `50%` finds "50%". |
| `multiWordSearch` | ✅ | ⚙️ `MultiWordSearch` | |
| `ignoreSearchByInQueryParam` | ✅ | ✅ `IgnoreSearchByInQueryParam` | |
| `filter.<field>=` with `$eq $gt $gte $lt $lte $in $btw $null $ilike $sw $contains` | ✅ | ✅ | |
| `$not` suffix | must be allowed | ⚠️ always allowed | Allowed with every allowed operator. |
| `filterableColumns: { field: true }` | ✅ | ✅ `AllOperators` | On a field with a `Type`, the operators that fit it: `$contains` only on arrays. |
| Empty operator list: `$eq` only | ✅ | ✅ | `Filter: []paginate.Operator{paginate.OpEq}`; a nil `Filter` is not filterable. |
| Typed comparison (numbers, dates, uuids) | ✅ | ✅ | `CompareAsText` compares `$eq` and `$in` as text instead. |
| `$contains`: array contains every value | ✅ | ✅ | `column @> '{...}'`. |
| `$btw` with one value | no rows: the upper bound is NULL | ➕ lower bound | |
| `$lte:YYYY-MM-DD` covers the whole day | ✅ | ❌ | Ends at midnight here; use the next day as the bound. |
| `$and`/`$or` inside `filter.<field>` | removed in 15.0 | ➕ kept | Semantics of nestjs-paginate 14. |
| `filter=<expression>` with AND, OR, NOT, parentheses, quotes | ✅ | ✅ | Same grammar, terms always validated, 100-node cap (`MaxFilterExpressionComplexity`). |
| Repeated `filter=` | ignored | ➕ ANDed | |
| `filter=` in `links` | dropped | ➕ kept | Following a link keeps the expression. |
| Empty `filter.x=` | filters by `''` | ⚠️ ignored | Inside `filter=`, `x=` filters by `''` as in nestjs. |
| `withDeleted=true` | `allowWithDeletedInQuery` | ⚙️ `AllowWithDeletedInQuery` | GORM only. |
| `cursor=` | `paginationType: CURSOR` | ⚙️ `CursorPagination` | See [cursor pagination](#cursor-pagination) below. |
| `select=` | ✅ | ❌ | The base query's `Select` and the row type choose the columns. |
| Relations: `relations`, to-many `EXISTS`, `$any`/`$all`/`$none` | ✅ | ❌ | The base query joins or preloads; fields map API names to joined columns. |
| JSONB key paths, polymorphic `a~b` columns | ✅ | ⚠️ by hand | A field's `Column` may be `data->>'key'` or `COALESCE(a, b)`. |
| Caps on search length and filter values | ❌ | ➕ | `MaxSearchLength`, `MaxFilterConditions`, `MaxFilterValues`. |
| `paginationMode=default\|count\|disabled` | ❌ | ➕ | |

## Cursor pagination

Both answer a cursor page the same way: the client sends `?cursor=` from a link, `meta.cursor` echoes
it, `links` has `previous`, `current` and `next`, and nothing is counted. The cursor itself and the
query behind it differ. [Pages and counts](pagination.md) explains cursor pagination here.

| Feature | nestjs-paginate 16.0.1 | paginate | Notes |
|---|---|---|---|
| Cursor pagination | `paginationType: CURSOR` | ⚙️ `Pagination: CursorPagination` | `page` is ignored. |
| `meta.cursor`, links `previous`, `current`, `next`, no totals | ✅ | ✅ | With `FormatNestJS`, `meta.itemsPerPage` is the number of rows on the page, as in nestjs-paginate. |
| The cursor | the sort values as a sortable string (`V998328469600000`) | ⚠️ opaque | Base64url JSON with the values of the boundary row and the sort it was made for. Clients pass it back as is. |
| The query | `WHERE <string> < :cursor`, `ORDER BY` that string | ⚠️ keyset | `WHERE` and `ORDER BY` name the sort columns, so an index on them serves the order; nestjs-paginate builds the string for every matching row and sorts by it. The condition starts with a range on the main key (`k <= $1`), where Postgres starts an index scan, when no NULL value of that key comes after the cursor: a `NotNull` field, or NULL values first in the direction of the page; otherwise a deep page reads the index from its start, as `OFFSET` does. See [speed](pagination.md#speed). |
| Sort columns | numbers and dates | ➕ text too | UUIDs and booleans too. Values are not rounded; nestjs-paginate keeps 11 integer and 4 decimal digits. |
| A unique column | must be in `sortBy` | ⚠️ `TieBreak` | Required: `Compile` fails without it. It ends every sort, so clients need not send it. |
| NULL values | always last | ⚠️ `NullSort` | `nullSort` does not apply to nestjs-paginate's cursor pages. Here NULL values go where the sort puts them, on the way back too. |
| The `previous` link | reverses `sortBy` | ⚠️ keeps `sortBy` | The cursor carries the direction, so the previous page comes in the same order as the others. |
| `previous` on the first page, `next` on the last | linked whenever the page has rows | ⚠️ only to existing rows | The executor reads one row more than the page to tell whether a next page exists. An empty page links back to the rows before it. |
| A cursor sent with another `sortBy` | compared anyway | ➕ 400 | `invalid_cursor`, like a broken cursor. |
| `limit=0` on a cursor endpoint | no total in meta | ➕ the count | |

## Answer

| Feature | nestjs-paginate 16.0.1 | paginate | Notes |
|---|---|---|---|
| `{data, meta}` | ✅ | ✅ | |
| `meta.itemsPerPage/totalItems/currentPage/totalPages` | ✅ | ✅ | `currentPage` within `[1, totalPages]`. |
| `meta.sortBy` as `[["field", "ASC"]]`, effective sort | ✅ | ✅ | In both formats, the default sort included. |
| `meta.filter` with a string for one value | ✅ | ⚙️ `FormatNestJS` | The default format always has arrays. |
| `meta.search`, `meta.searchBy` only with a search | ✅ | ⚙️ `FormatNestJS` | The default format always has both and echoes the request's `searchBy`. |
| `links`: first, previous, current, next, last | ✅ | ⚙️ `FormatNestJS` | `meta.Links()` gives them in any format. Values are URL-encoded here, with `$ : , ( )` kept readable; nestjs-paginate writes them raw. |
| Absolute links from the request's host | ✅ | ⚠️ `LinksBaseURL` | Links are relative unless `LinksBaseURL` is set; the Host header is never trusted. |
| `origin`, `relativePath` | ✅ | ✅ `LinksBaseURL` | |
| `meta.cursor` | ✅ | ✅ | In cursor pagination. |
| `meta.select` | ✅ | ❌ | |
| Rows mapped to DTOs, meta and links kept | by hand | ✅ `paginate.Map` | |
| `meta.paginationMode`, `hasNextPage`, `hasPreviousPage`, `nextCursor`, `previousCursor`, `estimated` | ❌ | ➕ default format | |
| The answer to a client's mistake | Nest's `{statusCode, message, error}` | ⚠️ `{code, field, message}` | `*paginate.Error`; `paginate.HTTPStatus` picks 400 or 500. Both bodies have `message`. See [Errors](errors.md). |

## Configuration and tooling

| Feature | nestjs-paginate 16.0.1 | paginate | Notes |
|---|---|---|---|
| `sortableColumns`, `searchableColumns`, `filterableColumns` | ✅ | ⚠️ fields | A `Field` names the API field, its column or SQL expression and its `Type`, and says what clients may do with it: `Sortable`, `Searchable`, `Filter`. See [Configuration](configuration.md). |
| The config on the model | ❌ | ➕ tags | `paginate.ConfigOf`, `gormx.ConfigOf`: `paginate:"sort;search;filter:$in"`. `gormx.NewBuilder` takes model and DTO fields. |
| `defaultSortBy`, `nullSort` | ✅ | ✅ `DefaultSort`, `NullSort` | |
| Default sort `[[sortableColumns[0], 'ASC']]` | ✅ | ✅ | The first sortable field, ascending. |
| Tie-breaker on a unique column | ❌ | ➕ `TieBreak` | Stable pages; cursor pagination requires it. |
| `defaultLimit`, `maxLimit` | ✅ | ✅ | `DefaultLimit`, `MaxLimit`. |
| `where` | ✅ | ✅ | `Where` on the base query. |
| `withDeleted` | ✅ | ✅ | `Unscoped()` on the GORM base query. |
| Query builder | ✅ | ✅ | A GORM query or a squirrel builder. |
| `paginationType: TAKE_AND_SKIP` (the default) | pages by entity | ⚠️ `LIMIT`, `OFFSET` | A to-many join repeats rows here: filter by it with `EXISTS`, load it with `Preload`. |
| Global defaults (`updateGlobalConfig`) | ✅ | ⚠️ | A function that sets your defaults before `Compile`. |
| `throwOnInvalidFilter: true` | ✅ | ⚠️ the default | Here it covers `sortBy` and `searchBy` too. `Lenient` drops what nestjs-paginate drops by default. |
| Exact count | ✅ | ✅ `CountExact` | |
| `buildCountQuery`, `optimizedCount` | ✅ | ⚠️ | No hook: gormx counts with GORM's `Count`, squirrelx wraps the query in `SELECT COUNT(*)`. |
| Estimated count | ❌ | ➕ `CountEstimate` | The planner's estimate from `EXPLAIN`; `ExactCountBelow` counts small results exactly. |
| No count | cursor pages only | ➕ `CountNone` | Page numbers without totals: one extra row tells whether a next page exists. |
| The count and the page at once | ❌ | ➕ `ParallelCount` | On two connections: GORM outside a transaction, squirrelx on a `*pgxpool.Pool`. Elsewhere one after the other. |
| Config check | per request: 503 without `sortableColumns` | ➕ `Compile` | Every mistake at once, at startup. |
| Swagger decorators | ✅ | ⚠️ | A hand-written [OpenAPI](use-cases/openapi.md) fragment; `Endpoint.Fields()` lists the fields, their types and operators. |
| HTTP frameworks | Express, Fastify | net/http (chi, any router), fasthttp | `ParseQuery` for any other transport. |
| Databases | TypeORM drivers | Postgres | SQL uses `ILIKE`, `@>`, `CAST` and `EXPLAIN`. |
