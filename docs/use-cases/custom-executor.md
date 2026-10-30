# A custom executor

`Endpoint.Prepare` does everything but running SQL: it checks the request against the endpoint and
returns a `Query` with the conditions, the sort, the page and how to count. `paginate.NewResponse`
turns what the executor read into the answer. An executor for another library runs the `Query` in
between, the way `gormx` and `squirrelx` do. Here is one for `database/sql` on Postgres, following
the steps `Query` documents.

```go
package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/svyatoslavratov/go-paginate"
)

// Paginate runs a paginated query on Postgres through database/sql. from is the SELECT with its
// joins, without WHERE, ORDER BY or LIMIT. scope holds the conditions every page keeps, such as the
// tenant, with "?" placeholders like paginate's own. scan reads a row; in cursor pagination T must
// carry the sort fields.
func Paginate[T any](ctx context.Context, db *sql.DB, req paginate.Request, endpoint *paginate.Endpoint,
	from string, scope []paginate.Condition, scan func(*sql.Rows) (T, error)) (*paginate.Response[T], error) {
	q, err := endpoint.Prepare(&req)
	if err != nil {
		return nil, err // the client's mistake: paginate.HTTPStatus(err) is 400
	}

	// 1. AND the conditions of the request to the scope.
	var conditions []string
	var args []any
	for _, condition := range scope {
		conditions = append(conditions, "("+condition.SQL+")")
		args = append(args, condition.Args...)
	}
	for _, condition := range q.Where {
		conditions = append(conditions, condition.SQL)
		args = append(args, condition.Args...)
	}
	query := from
	if len(conditions) != 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// 2. Count the matching rows the way q.Count says.
	total, err := count(ctx, db, q, query, args)
	if err != nil {
		return nil, err
	}
	if q.Mode == paginate.ModeCount {
		return paginate.NewResponse[T](q, nil, total)
	}

	// 3. Read the rows in order: a page, or every row in ModeDisabled.
	query += " ORDER BY " + strings.Join(q.OrderBy(true), ", ")
	if q.Mode == paginate.ModeDefault {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", q.FetchLimit(), q.Offset)
	}
	rows, err := db.QueryContext(ctx, rebind(query), args...)
	if err != nil {
		return nil, q.WrapError("select", err)
	}
	defer rows.Close()
	var data []T
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return nil, err
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, q.WrapError("select", err)
	}

	// 4. The answer: the meta, the links and, in cursor pagination, the cursors.
	return paginate.NewResponse(q, data, total)
}

// count counts the rows of query: COUNT(*), the planner's estimate, or nothing.
func count(ctx context.Context, db *sql.DB, q *paginate.Query, query string, args []any) (paginate.Total, error) {
	switch q.Count {
	case paginate.CountNone:
		return paginate.Total{}, nil
	case paginate.CountEstimate:
		var plan []byte
		if err := db.QueryRowContext(ctx, rebind(paginate.ExplainPrefix+query), args...).Scan(&plan); err != nil {
			return paginate.Total{}, q.WrapError("estimate", err)
		}
		estimate, err := paginate.EstimateFromPlan(plan)
		if err != nil {
			return paginate.Total{}, err
		}
		if estimate >= q.ExactCountBelow {
			return paginate.Total{Items: estimate, Count: paginate.CountEstimate}, nil
		}
	}
	var n uint64
	if err := db.QueryRowContext(ctx, rebind("SELECT COUNT(*) FROM ("+query+") AS c"), args...).Scan(&n); err != nil {
		return paginate.Total{}, q.WrapError("count", err)
	}
	return paginate.Total{Items: n, Count: paginate.CountExact}, nil
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

A handler calls it with the request it parsed:

```go
page, err := repo.Paginate(r.Context(), db, *req, usersEndpoint,
	"SELECT users.id, users.name, users.created_at FROM users",
	[]paginate.Condition{{SQL: "users.tenant_id = ?", Args: []any{tenantID}}},
	func(rows *sql.Rows) (User, error) {
		var u User
		err := rows.Scan(&u.ID, &u.Name, &u.CreatedAt)
		return u, err
	})
```

For `?page=2&limit=2&filter.status=$in:active,invited` on an endpoint sorted by `createdAt`
descending, it runs:

```sql
SELECT COUNT(*) FROM (SELECT users.id, users.name, users.created_at FROM users WHERE (users.tenant_id = $1) AND users.status IN ($2, $3)) AS c
SELECT users.id, users.name, users.created_at FROM users WHERE (users.tenant_id = $1) AND users.status IN ($2, $3) ORDER BY users.created_at DESC, users.id ASC LIMIT 2 OFFSET 2
```

What every executor must do:

- AND every `q.Where` condition with the base query; each is already parenthesized where needed.
  Wrap OR conditions of the base query in parentheses too, as the `scope` loop above does.
- Count over the filtered query without `ORDER BY` and `LIMIT`, the way `q.Count` says: `COUNT(*)`
  for `CountExact`; for `CountEstimate`, `EXPLAIN` (`paginate.ExplainPrefix`) read by
  `paginate.EstimateFromPlan`, with an exact count when the estimate is below `q.ExactCountBelow`;
  nothing for `CountNone`, which is also what cursor pages and every-row requests get. Pass what
  was counted to `NewResponse` as a `paginate.Total`.
- Return `NewResponse[T](q, nil, total)` for `ModeCount`, without reading rows.
- Read with `LIMIT q.FetchLimit()`, not `q.Limit`: without a count and in cursor pagination it is
  one row more, which tells whether there is a next page. Add `OFFSET q.Offset`. Skip both for
  `ModeDisabled`.
- Order by `q.OrderBy(withTieBreak)`; it is already reversed when the client pages back from a
  cursor. Pass `false` only where the database rejects the tie-breaker (`DISTINCT`, `GROUP BY`
  without that column), and never when `q.Keyset` is set: cursor pagination needs it, so refuse
  such a query, as `gormx` does.
- Run `WithDeleted` requests without the soft-delete filter, if the executor has one.
- Wrap database errors with `q.WrapError`, so a value of the wrong type is a 400.
- Build the answer with `paginate.NewResponse(q, rows, total)` from every row read. It drops the
  extra row, turns the rows of a backward page around, and makes the cursors and the links.
- `q.Parallel` asks to run the count and the page at the same time. An `*sql.DB` is a pool and can;
  a transaction is one connection and cannot.

See [pages and counts](../pagination.md) for what the count modes and cursors cost.

`rebind` above is naive: a `?` inside a string literal of the base query would be replaced too.
Keep literals out of the base query, or use your library's own rebinding.
