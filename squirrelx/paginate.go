// Package squirrelx runs paginate requests on a squirrel select with pgx.
package squirrelx

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

// Plan is what Paginate runs for a request; a statement it does not need is empty.
type Plan struct {
	// Count counts the matching rows. Under CountEstimate it runs only for estimates below
	// ExactCountBelow; without a count it is empty.
	Count Statement
	// Estimate asks the planner for the number of matching rows, under CountEstimate.
	Estimate Statement
	// Page reads the rows; it is empty for the count only.
	Page Statement
}

// Paginate runs a paginated query built with squirrel. qb carries the columns, the table, joins
// and the conditions of the endpoint; the request adds search, filters, sorting and the page. scan
// reads the rows, for example func(rows pgx.Rows) ([]T, error) { return pgx.CollectRows(rows,
// pgx.RowToStructByName[T]) }; in cursor pagination the rows must carry the sort fields. The count
// runs alongside the page only on a *pgxpool.Pool. Errors wrap paginate.ErrInvalidRequest for the
// client's mistakes; paginate.HTTPStatus picks the status.
func Paginate[T any](
	ctx context.Context,
	req paginate.Request,
	endpoint *paginate.Endpoint,
	db Querier,
	scan func(rows pgx.Rows) ([]T, error),
	qb squirrel.SelectBuilder,
) (*paginate.Response[T], error) {
	if endpoint == nil || db == nil {
		return nil, errors.New("squirrelx: endpoint and db are required")
	}
	q, err := endpoint.Prepare(&req)
	if err != nil {
		return nil, err
	}
	plan, err := statements(qb, q)
	if err != nil {
		return nil, err
	}

	var total paginate.Total
	count := func() (err error) {
		total, err = countRows(ctx, db, q, plan)
		return err
	}
	if q.Mode == paginate.ModeCount {
		if err := count(); err != nil {
			return nil, err
		}
		return paginate.NewResponse[T](q, nil, total)
	}

	var data []T
	read := func() error {
		rows, err := db.Query(ctx, plan.Page.SQL, plan.Page.Args...)
		if err != nil {
			return q.WrapError("select", err)
		}
		defer rows.Close()
		if data, err = scan(rows); err != nil {
			return q.WrapError("scan", err)
		}
		return nil
	}

	_, pool := db.(*pgxpool.Pool)
	switch {
	case q.Count == paginate.CountNone:
		err = read()
	case q.Parallel && pool:
		var wg sync.WaitGroup
		var countErr error
		wg.Go(func() { countErr = count() })
		readErr := read()
		wg.Wait()
		err = errors.Join(readErr, countErr)
	default:
		if err = count(); err == nil {
			err = read()
		}
	}
	if err != nil {
		return nil, err
	}
	return paginate.NewResponse(q, data, total)
}

// countRows counts the rows the way q says: COUNT(*), or the planner's estimate, counted exactly
// below q.ExactCountBelow.
func countRows(ctx context.Context, db Querier, q *paginate.Query, plan Plan) (paginate.Total, error) {
	if q.Count == paginate.CountEstimate {
		var output []byte
		if err := db.QueryRow(ctx, plan.Estimate.SQL, plan.Estimate.Args...).Scan(&output); err != nil {
			return paginate.Total{}, q.WrapError("estimate", err)
		}
		estimate, err := paginate.EstimateFromPlan(output)
		if err != nil {
			return paginate.Total{}, err
		}
		if estimate >= q.ExactCountBelow {
			return paginate.Total{Items: estimate, Count: paginate.CountEstimate}, nil
		}
	}
	var n uint64
	if err := db.QueryRow(ctx, plan.Count.SQL, plan.Count.Args...).Scan(&n); err != nil {
		return paginate.Total{}, q.WrapError("count", err)
	}
	return paginate.Total{Items: n, Count: paginate.CountExact}, nil
}

// Statements checks the request and renders what Paginate runs for it, for tests and for running
// the statements elsewhere. It normalizes req in place, as Endpoint.Prepare does.
func Statements(qb squirrel.SelectBuilder, req *paginate.Request, endpoint *paginate.Endpoint) (Plan, error) {
	q, err := endpoint.Prepare(req)
	if err != nil {
		return Plan{}, err
	}
	return statements(qb, q)
}

// statements adds the query to qb. The count wraps the query as a subquery, so GROUP BY, DISTINCT
// and ORDER BY of qb count right. squirrel joins Where parts with a bare AND, so OR conditions of
// qb must be wrapped in squirrel.Or or parentheses; the conditions added here always are. squirrel
// cannot show the shape of qb, so the tie-breaker is always appended: leave the TieBreak of the
// config empty for DISTINCT and GROUP BY queries.
func statements(qb squirrel.SelectBuilder, q *paginate.Query) (Plan, error) {
	for _, condition := range q.Where {
		qb = qb.Where(squirrel.Expr(condition.SQL, condition.Args...))
	}
	var plan Plan
	var err error
	if q.Count != paginate.CountNone {
		if plan.Count, err = render(squirrel.Select("COUNT(*)").FromSelect(qb, "paginate_count"), "count"); err != nil {
			return Plan{}, err
		}
	}
	if q.Count == paginate.CountEstimate {
		if plan.Estimate, err = render(qb, "estimate"); err != nil {
			return Plan{}, err
		}
		plan.Estimate.SQL = paginate.ExplainPrefix + plan.Estimate.SQL
	}
	if q.Mode != paginate.ModeCount {
		page := qb
		if q.Mode == paginate.ModeDefault {
			page = page.Limit(q.FetchLimit()).Offset(q.Offset)
		}
		if orderBy := q.OrderBy(true); len(orderBy) != 0 {
			page = page.OrderBy(orderBy...)
		}
		if plan.Page, err = render(page, "select"); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

func render(qb squirrel.SelectBuilder, stage string) (Statement, error) {
	sql, args, err := qb.PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return Statement{}, fmt.Errorf("squirrelx: build %s: %w", stage, err)
	}
	return Statement{SQL: sql, Args: args}, nil
}
