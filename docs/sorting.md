# Sorting and tie-breaker

## Key priority

The `ORDER BY` of a request is built in this order:

1. The `sortBy` keys of the request, in order. `Request.SortBy` is a list, so the order survives a
   request built in code or passed to another service as JSON:
   `[["score","DESC"],["createdAt","ASC"]]`. A repeated field counts once, at its first position.
2. Without request keys: `DefaultSort`, in its order.
3. Without `DefaultSort`: the first sortable field of `Fields`, ascending.
4. The tie-breaker, last.

Request keys replace the default sort; they are not added to it. A key on a field that is unknown
or not sortable is a 400 with `sort_not_allowed`. With `Lenient` it is dropped, and when no key is
left the default sort applies.

`DefaultSort` names API fields, like a request:

```go
cfg.DefaultSort = []paginate.Sort{
	{Field: "pinned", Order: paginate.Desc},
	{Field: "createdAt", Order: paginate.Desc},
}
```

```sql
ORDER BY posts.pinned DESC, posts.created_at DESC, posts.id ASC
```

Any field may be a default key, sortable or not: the default sort may use `pinned` even when
clients cannot sort by it. `Compile` refuses a default key on an unknown field or with an order
other than `ASC` and `DESC`, and an endpoint with no sort at all: no `DefaultSort`, no sortable
field and no tie-breaker.

The meta reports the effective sort in API names, the default included and the tie-breaker left
out, so a client can render its sort controls from it:

```json
"sortBy":[["pinned","DESC"],["createdAt","DESC"]]
```

## NULLS FIRST / LAST

`NullSort: paginate.NullsLast` (or `NullsFirst`) adds `NULLS LAST` to every key, the tie-breaker
included:

```sql
ORDER BY products.rating DESC NULLS LAST, products.id ASC NULLS LAST
```

Without it, Postgres puts NULL last in ascending order and first in descending order: products
without a rating come first under `sortBy=rating:DESC`. Mind the indexes: a plain index on
`rating` serves `rating DESC`, but not `rating DESC NULLS LAST`, which needs an index declared that
way.

Cursor pagination keeps the same placement. The keyset condition steps over NULL values where the
sort puts them, and a page read back from a cursor reverses every key together with its NULL
placement, which is then always written out:

```sql
ORDER BY posts.created_at ASC NULLS LAST, posts.id DESC NULLS FIRST
```

## Tie-breaker

`LIMIT/OFFSET` pages are stable only when `ORDER BY` decides between every pair of rows. With
`ORDER BY created_at` and two rows of one timestamp (one `INSERT` of many rows, or `DEFAULT NOW()` in
one transaction), Postgres may return them in either order on every query. One row then shows up on
two pages and another on none.

`TieBreak` names a unique column appended as the last key, ascending:

```go
cfg.TieBreak = "users.id"
```

```sql
ORDER BY users.created_at DESC, users.id ASC
```

A tie-breaker that is already a key is not added again: `sortBy=id:DESC` is
`ORDER BY users.id DESC`.

- The `unique` tag marks it. Without one, `gormx.ConfigOf` takes the model's primary key, and so
  does the GORM builder (single-column keys only). `b.TieBreak(&m.Slug)` picks another unique
  column; `b.WithoutTieBreak()` turns it off.
- `gormx.Paginate` skips it when Postgres would reject it: for `DISTINCT` (including a raw
  `Select("DISTINCT ...")`) and for `GROUP BY` that does not group by this column.
- `squirrelx.Paginate` cannot see the query's shape and always appends it. Leave it empty for
  `DISTINCT` and `GROUP BY` queries.
- A table without a single unique column (a composite key) needs no tie-breaker if the sort keys
  already cover the key; otherwise sort by all key columns.

Name the column the way the query does. With `Table("templates AS t")` the tie-breaker is `t.id`,
not `templates.id`.

### In cursor pagination

Cursor pagination cannot do without the tie-breaker: `Compile` refuses `CursorPagination` without a
`TieBreak`. A cursor holds the values of the sort keys and the tie-breaker of the row it marks, and
the next page is the rows after that row in the sort order:

```sql
-- sortBy=createdAt:DESC, after the row created at 2024-05-02T10:00:00Z with id 7
((users.created_at < $1) OR ((users.created_at = $2) AND (users.id > $3)))
```

The tie-breaker makes "after that row" exact: rows with the same `created_at` are told apart by
`id`, so none is skipped or repeated at a page boundary. For the same reason it must end the
`ORDER BY`: `gormx.Paginate` refuses cursor pagination of a `DISTINCT` or `GROUP BY` query that
cannot have it, instead of skipping it.

The tie-breaker need not be an API field, but the rows must carry it. `NewResponse` finds it by
its API name when it is a field, otherwise by the last part of the column (`id` for `users.id`): a
`db` or gorm `column` tag, or the snake_case of a Go field name. See
[cursors](pagination.md#cursors).
