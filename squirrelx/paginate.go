// Package squirrelx runs paginate requests on a squirrel select with pgx.
package squirrelx

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/svyatoslavratov/go-paginate"
)

// Querier runs SQL: a *pgxpool.Pool, a *pgx.Conn or a pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Statement is SQL with $n placeholders and its arguments.
type Statement struct {
	SQL  string
	Args []any
}

// Paginate runs a paginated query built with squirrel. qb carries the columns, the table, joins
// and the conditions of the endpoint; the request adds search, filters, sorting and the page. scan
// reads the rows, for example func(rows pgx.Rows) ([]T, error) { return pgx.CollectRows(rows,
// pgx.RowToStructByName[T]) }. Errors wrap paginate.ErrInvalidRequest for the client's mistakes.
func Paginate[T any](
	ctx context.Context,
	req paginate.Request,
	cfg paginate.Config,
	db Querier,
	scan func(rows pgx.Rows) ([]T, error),
	qb squirrel.SelectBuilder,
) (*paginate.Response[T], error) {
	q, err := paginate.Prepare(&req, cfg)
	if err != nil {
		return nil, err
	}
	count, page, err := statements(qb, q)
	if err != nil {
		return nil, err
	}

	var total uint64
	if err := db.QueryRow(ctx, count.SQL, count.Args...).Scan(&total); err != nil {
		return nil, q.WrapError("count", err)
	}
	if q.Mode == paginate.ModeCount {
		return paginate.NewResponse[T](nil, req, total), nil
	}

	rows, err := db.Query(ctx, page.SQL, page.Args...)
	if err != nil {
		return nil, q.WrapError("select", err)
	}
	defer rows.Close()
	data, err := scan(rows)
	if err != nil {
		return nil, q.WrapError("scan", err)
	}
	return paginate.NewResponse(data, req, total), nil
}

// Statements checks the request and renders the count and the page statements Paginate runs, for
// tests and for running them elsewhere. It normalizes req in place, as paginate.Prepare does.
func Statements(qb squirrel.SelectBuilder, req *paginate.Request, cfg paginate.Config) (count, page Statement, err error) {
	q, err := paginate.Prepare(req, cfg)
	if err != nil {
		return Statement{}, Statement{}, err
	}
	return statements(qb, q)
}

// statements adds the query to qb. The count wraps the query as a subquery, so GROUP BY, DISTINCT
// and ORDER BY of qb count right. squirrel joins Where parts with a bare AND, so OR conditions of
// qb must be wrapped in squirrel.Or or parentheses; the conditions added here always are. squirrel
// cannot show the shape of qb, so the tie-breaker is always appended: leave Config.TieBreakColumn
// empty for DISTINCT and GROUP BY queries.
func statements(qb squirrel.SelectBuilder, q *paginate.Query) (count, page Statement, err error) {
	for _, condition := range q.Where {
		qb = qb.Where(squirrel.Expr(condition.SQL, condition.Args...))
	}
	countQb := squirrel.Select("COUNT(*)").FromSelect(qb, "paginate_count")
	if q.Mode == paginate.ModeDefault {
		qb = qb.Limit(q.Limit).Offset(q.Offset)
	}
	if orderBy := q.OrderBy(true); len(orderBy) != 0 {
		qb = qb.OrderBy(orderBy...)
	}

	countSQL, countArgs, err := countQb.PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return Statement{}, Statement{}, fmt.Errorf("squirrelx: build count: %w", err)
	}
	pageSQL, pageArgs, err := qb.PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return Statement{}, Statement{}, fmt.Errorf("squirrelx: build select: %w", err)
	}
	return Statement{SQL: countSQL, Args: countArgs}, Statement{SQL: pageSQL, Args: pageArgs}, nil
}
