# Pages and counts

An endpoint pages in one of two ways, and counts in one of three. Both are settings of the
`Config`, so the server, not the client, decides what a list may cost.

## Page numbers

`?page=3&limit=20` is `LIMIT 20 OFFSET 40`: the database reads the 40 rows before the page and
throws them away. That is the default, `OffsetPagination`, and what nestjs-paginate does unless
told otherwise.

- Clients can open any page and show "page 3 of 12".
- Deep pages get slower: page 5 000 reads 100 000 rows to return 20.
- Rows inserted or deleted while a client pages shift the pages: a row can show up twice, or not
  at all. A unique tie-breaker keeps the order stable, but not the contents of a page.

## Cursors

`CursorPagination` is keyset pagination. A cursor marks the last row the client saw; the next page
is the rows after it in the sort order, found with `WHERE` instead of `OFFSET`:

```sql
-- sortBy=createdAt:DESC and the tie-breaker users.id, after the row (2024-05-02 10:00, 7)
WHERE ((users.created_at <= $1) AND ((users.created_at < $2) OR ((users.created_at = $3) AND (users.id > $4))))
ORDER BY users.created_at DESC, users.id ASC
LIMIT 21
```

- A deep page costs what the first one does: with an index on the sort keys, Postgres starts the
  scan right at the cursor (`users.created_at <= $1` is that range). That holds when the main
  sort key has no NULL values to step around; see [speed](#speed).
- Rows that appear or disappear elsewhere do not shift the pages: the next page starts after the
  row the client saw, whatever happened before it.
- Clients move to the next or the previous page only. There are no page numbers and no total.

It suits feeds, infinite scrolling, exports and syncs; page numbers suit admin tables where
people jump to page 7.

```go
cfg.Pagination = paginate.CursorPagination
cfg.TieBreak = "users.id" // required
```

The first request has no cursor. The answer leads on through `links.next` (nestjs format) or
`meta.nextCursor` (default format); the client sends it back as `?cursor=` with the same sort and
filters, which the links already carry:

```json
"meta": {"itemsPerPage": 20, "sortBy": [["createdAt", "DESC"]]},
"links": {
  "current": "/users?limit=20&sortBy=createdAt:DESC",
  "next": "/users?limit=20&sortBy=createdAt:DESC&cursor=WyJuIiwiY3JlYXRlZEF0OkRFU0MsaWQ6QVNDIiwiMjAyNC0wNS0wMlQxMDowMDowMFoiLDdd"
}
```

### How cursors work

- A cursor is base64url-encoded JSON: the direction, the sort it was made for, and the values of
  the sort keys of the marked row. Clients treat it as opaque.
- It is tied to its sort. A cursor sent with another `sortBy` is a 400 with `invalid_cursor`, not
  a silently wrong page. So is a cursor that does not decode, or whose values do not fit the
  fields' types. The values are bind parameters like any filter value.
- The executor reads one row more than the page (`Query.FetchLimit`), so the answer knows whether
  a next page exists. `next` is linked only when it does.
- `previous` pages back from the first row of the page: the executor reads in the reverse order,
  and `NewResponse` turns the rows back, so a client sees every page in the same order.
- An empty page (the rows after the cursor are gone) links back to the page that ends with the
  cursor's row.
- NULL values sort where `NullSort` (or the database default) puts them, and the cursor steps
  over them correctly in both directions.

### Speed

The `OR` of a keyset condition is no index range for Postgres: alone, it is a filter, and a deep
page would read the index from its start, the way `OFFSET` does. So the condition starts with a
range on the main sort key, `k >= v` going up or `k <= v` going down, which says the same thing
and is where an index scan starts. The range holds only when no NULL value of the key comes after
the cursor's value:

- the field is `NotNull` (a column without NULL values: tag `notnull`, or GORM's `not null` and
  primary keys through `gormx`);
- or NULL values come before the values in the direction of the page: the database default for
  `DESC` going forward, or `NullSort: NullsFirst`.

Paging back flips the direction, so only `NotNull` keeps both directions fast. A million rows,
an index on `(created_at DESC, id)`, a page at a depth of 855 000:

| Query | Time |
|---|---:|
| `OFFSET 855000` | 404 ms |
| keyset without the range | 382 ms |
| keyset, `created_at DESC`, forward | 0.4 ms |
| keyset, `created_at DESC`, back: nullable / `NotNull` | 146 ms / 0.2 ms |
| keyset, `rank ASC` with NULL values last: nullable / `NotNull` | 1 188 ms / 0.1 ms |

The index must follow the sort, NULL placement included: `NullsFirst` on an ascending key needs
an index built `NULLS FIRST`.

### What it needs

- A `TieBreak`: the sort keys plus a unique column order the rows completely, which a cursor needs
  to point between two rows.
- Rows that carry the sort fields: `NewResponse` reads the cursor values from the first and the
  last row. It finds them by the field's API name (the JSON name of a struct field, a key of a
  map or a JSON object), or by the column's name (a `db` or gorm `column` tag, or the snake_case
  of the Go name). A missing one is an error that names it.
- Sort keys that are columns, not aggregates: `WHERE` cannot compare an aggregate. A `GROUP BY` or
  `DISTINCT` query must keep the tie-breaker in its `ORDER BY`; `gormx` refuses one that cannot.
- An index on the sort keys and the tie-breaker, in the order of the sort, and `NotNull` on the
  sort fields that have no NULL values, for the speed (see [speed](#speed)).

The answer has no totals. A count-only request (`limit=0`) still counts, and `limit=-1` still
reads every row: cursors are for pages.

## Counting

Page numbers need a total: `totalItems`, `totalPages` and the link to the last page. `COUNT(*)`
reads every matching row, which on a big table costs more than the page itself. `Config.Count`
chooses:

| `Count` | What runs | The answer |
|---|---|---|
| `CountExact` (default) | `SELECT COUNT(*)` of the matching rows | exact totals |
| `CountEstimate` | `EXPLAIN (FORMAT JSON)` of the matching rows: the planner's estimate, no rows read | totals marked `"estimated": true`; no `last` link |
| `CountNone` | nothing; the page reads one row more | no totals; `hasNextPage` from the extra row; no `last` link |

```go
cfg.Count = paginate.CountEstimate
cfg.ExactCountBelow = 10_000 // counted exactly when the estimate is smaller
```

An estimate is as good as the table statistics (`ANALYZE`, autovacuum) and usually rougher with
filters. `ExactCountBelow` covers the common case: big lists get the cheap estimate, small or
heavily filtered ones an exact number.

`CountNone` suits "load more" lists and pages that do not show a total. A count-only request
(`limit=0`) counts exactly even under `CountNone`: counting is all it asks for.

### Counting alongside the page

`ParallelCount` runs the count and the page at the same time, on two connections, so the answer
takes as long as the slower of them instead of their sum:

```go
cfg.ParallelCount = true
```

A transaction has one connection, so inside one the bundled executors run them one after the
other: `gormx` when its `*gorm.DB` is bound to a transaction, `squirrelx` unless its `Querier` is a
`*pgxpool.Pool`.

## In the answer

| | Page numbers, counted | Page numbers, `CountNone` | Cursors |
|---|---|---|---|
| `totalItems`, `totalPages` | ✅ (`estimated` with `CountEstimate`) | — | — |
| `currentPage` | ✅ | ✅ | — |
| `hasNextPage`, `hasPreviousPage` (default format) | ✅ | ✅ | ✅ |
| `cursor`, `nextCursor`, `previousCursor` (default format) | — | — | ✅ |
| `links` (nestjs format) | first, previous, current, next, last | first, previous, current, next | previous, current, next |

## For executors of your own

`Query` says what to run: `Count` is the count for this request (`CountNone` for cursor pages and
for every row), `FetchLimit()` is the `LIMIT` with the extra row, `OrderBy` is already reversed for
a backward cursor, and `Where` already holds the keyset condition. `NewResponse(q, rows, total)`
does the rest. See [a custom executor](use-cases/custom-executor.md).
