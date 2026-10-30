# Sorting and tie-breaker

## Key priority

The `ORDER BY` of a request is built in this order:

1. The `sortBy` keys of the request, in parameter order. The HTTP parsers keep the order in
   `Request.SortKeys`.
2. Keys without a known order, such as a request built in code or received over gRPC, where
   `SortBy` is a map. They follow `SortableColumns` order.
3. Without request keys: `DefaultSortBy` in `DefaultSortKeys` order, the rest of it alphabetically.
4. Without defaults: the first sortable column, ascending.
5. The tie-breaker, last.

A key that is mapped but not in `SortableColumns` is dropped. `Validate` reports sortable columns no
API field reaches.

## NULLS FIRST / LAST

`NullSort: paginate.NullsLast` (or `NullsFirst`) adds `NULLS LAST` to every key. Without it,
Postgres puts NULL last in ascending order and first in descending order.

## Tie-breaker

`LIMIT/OFFSET` pages are stable only when `ORDER BY` decides between every pair of rows. With
`ORDER BY created_at` and two rows of one timestamp (one `INSERT` of many rows, or `DEFAULT NOW()` in
one transaction), Postgres may return them in either order on every query. One row then shows up on
two pages and another on none.

`TieBreakColumn` names a unique column appended as the last key, ascending:

```go
cfg.TieBreakColumn = "users.id"
```

```sql
ORDER BY users.created_at DESC, users.id ASC
```

- The GORM builder sets it to the primary key of the model (single-column keys only).
  `WithTieBreak(&m.Slug)` picks another unique column; `WithoutTieBreak()` turns it off.
- `gormx.Paginate` skips it when Postgres would reject it: for `DISTINCT` (including a raw
  `Select("DISTINCT ...")`) and for `GROUP BY` that does not group by this column.
- `squirrelx.Paginate` cannot see the query's shape and always appends it. Leave it empty for
  `DISTINCT` and `GROUP BY` queries.
- A table without a single unique column (a composite key) needs no tie-breaker if the sort keys
  already cover the key; otherwise sort by all key columns.

Name the column the way the query does. With `Table("templates AS t")` the tie-breaker is `t.id`,
not `templates.id`.
