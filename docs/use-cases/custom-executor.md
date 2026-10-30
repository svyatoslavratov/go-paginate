# A custom executor

`paginate.Prepare` does everything but running SQL: it checks the request against the config and
returns a `Query` with the conditions, the sort and the page. An executor for another library adds
them to its base query, the way `gormx` and `squirrelx` do. Here is one for
`database/sql`.

```go
// Paginate runs a paginated query over base, a SELECT without WHERE, ORDER BY or LIMIT, with
// "?" placeholders rebound to $n for Postgres.
func Paginate[T any](ctx context.Context, db *sql.DB, req paginate.Request, cfg paginate.Config,
	base string, scan func(*sql.Rows) (T, error)) (*paginate.Response[T], error) {
	q, err := paginate.Prepare(&req, cfg)
	if err != nil {
		return nil, err // errors.Is(err, paginate.ErrInvalidRequest): answer 400
	}

	where, args := "", []any{}
	for i, condition := range q.Where {
		if i == 0 {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += condition.SQL
		args = append(args, condition.Args...)
	}
	query := base + where

	var total uint64
	if err := db.QueryRowContext(ctx, rebind("SELECT COUNT(*) FROM ("+query+") AS c"), args...).Scan(&total); err != nil {
		return nil, q.WrapError("count", err)
	}
	if q.Mode == paginate.ModeCount {
		return paginate.NewResponse[T](nil, req, total), nil
	}

	query += " ORDER BY " + strings.Join(q.OrderBy(true), ", ")
	if q.Mode == paginate.ModeDefault {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", q.Limit, q.Offset)
	}
	rows, err := db.QueryContext(ctx, rebind(query), args...)
	if err != nil {
		return nil, q.WrapError("select", err)
	}
	defer rows.Close()

	var data []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		data = append(data, item)
	}
	if err := rows.Err(); err != nil {
		return nil, q.WrapError("select", err)
	}
	return paginate.NewResponse(data, req, total), nil
}

// rebind turns "?" placeholders into $1, $2, ...
func rebind(query string) string {
	var out strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			out.WriteString("$" + strconv.Itoa(n))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
```

What every executor must do:

- AND every `q.Where` condition with the base query; each is already parenthesized where needed.
  Wrap OR conditions of the base query in parentheses too.
- Count over the filtered query, without its `LIMIT`.
- Return `NewResponse(nil, …)` for `ModeCount`, skip `LIMIT/OFFSET` for `ModeDisabled`.
- Pass `withTieBreak` to `OrderBy` only where the database accepts the tie-breaker: not for
  `DISTINCT`, not for `GROUP BY` without that column.
- Run `WithDeleted` requests without the soft-delete filter, if the executor has one.
- Wrap database errors with `q.WrapError`, so a value of the wrong type is a 400.
- Build the answer with `paginate.NewResponse(data, req, total)` from the request `Prepare`
  normalized, so the meta and the links are right.

`rebind` above is naive: a `?` inside a string literal of the base query would be replaced too.
Keep literals out of the base query, or use your library's own rebinding.
