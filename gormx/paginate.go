// Package gormx runs paginate requests on GORM and builds paginate configs from GORM models.
package gormx

import (
	"context"
	"errors"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/svyatoslavratov/go-paginate"
)

// Paginate runs a paginated query on top of db: db carries the table, joins, Select, preloads and
// the conditions of the endpoint; the request adds search, filters, sorting and the page. T is the
// type Find scans rows into; in cursor pagination it must carry the sort fields. Errors wrap
// paginate.ErrInvalidRequest for the client's mistakes; paginate.HTTPStatus picks the status.
func Paginate[T any](ctx context.Context, req paginate.Request, endpoint *paginate.Endpoint, db *gorm.DB) (*paginate.Response[T], error) {
	if db == nil || endpoint == nil {
		return nil, errors.New("gormx: db and endpoint are required")
	}
	q, err := endpoint.Prepare(&req)
	if err != nil {
		return nil, err
	}

	query := db.WithContext(ctx)
	if q.WithDeleted {
		query = query.Unscoped()
	}
	for _, condition := range q.Where {
		query = query.Where(condition.SQL, condition.Args...)
	}
	withTieBreak := tieBreakAllowed(query.Statement, q.TieBreak)
	if q.Keyset && !withTieBreak {
		return nil, errors.New("gormx: cursor pagination needs the tie-breaker in ORDER BY, which a DISTINCT or GROUP BY query without it cannot have")
	}

	// Both sessions are cut before anything runs, so the count may run on its own goroutine.
	countQuery, page := query.Session(&gorm.Session{}), query.Session(&gorm.Session{})
	var total paginate.Total
	count := func() (err error) {
		total, err = countRows[T](countQuery, q)
		return err
	}
	if q.Mode == paginate.ModeCount {
		if err := count(); err != nil {
			return nil, err
		}
		return paginate.NewResponse[T](q, nil, total)
	}

	if q.Mode == paginate.ModeDefault {
		page = page.Limit(int(q.FetchLimit())).Offset(int(q.Offset))
	}
	for _, order := range q.OrderBy(withTieBreak) {
		page = page.Order(order)
	}
	var rows []T
	find := func() error {
		if err := page.Find(&rows).Error; err != nil {
			return q.WrapError("select", err)
		}
		return nil
	}

	switch {
	case q.Count == paginate.CountNone:
		err = find()
	case q.Parallel && !inTransaction(db):
		var wg sync.WaitGroup
		var countErr error
		wg.Go(func() { countErr = count() })
		findErr := find()
		wg.Wait()
		err = errors.Join(findErr, countErr)
	default:
		if err = count(); err == nil {
			err = find()
		}
	}
	if err != nil {
		return nil, err
	}
	return paginate.NewResponse(q, rows, total)
}

// inTransaction reports a db bound to a transaction: one connection, which cannot run two
// statements at once.
func inTransaction(db *gorm.DB) bool {
	_, ok := db.Statement.ConnPool.(gorm.TxCommitter)
	return ok
}

// countRows counts the rows of query the way q says: COUNT(*), or the planner's estimate, counted
// exactly below q.ExactCountBelow.
func countRows[T any](query *gorm.DB, q *paginate.Query) (paginate.Total, error) {
	if q.Count == paginate.CountEstimate {
		estimate, err := estimateRows[T](query)
		if err != nil {
			return paginate.Total{}, q.WrapError("estimate", err)
		}
		if estimate >= q.ExactCountBelow {
			return paginate.Total{Items: estimate, Count: paginate.CountEstimate}, nil
		}
	}
	counted := query.Session(&gorm.Session{})
	if query.Statement.Distinct || selectsDistinct(query.Statement) {
		// GORM's Count turns DISTINCT a, b into count(*) of every row; the subquery keeps it.
		counted = query.Session(&gorm.Session{NewDB: true}).Table("(?) AS paginate_count", query.Session(&gorm.Session{}))
	}
	var n int64
	if err := counted.Count(&n).Error; err != nil {
		return paginate.Total{}, q.WrapError("count", err)
	}
	return paginate.Total{Items: uint64(n), Count: paginate.CountExact}, nil
}

// estimateRows asks the planner how many rows the query returns. GORM renders the query in dry-run
// mode; EXPLAIN goes straight to the connection, which takes the $n placeholders as they are.
func estimateRows[T any](query *gorm.DB) (uint64, error) {
	stmt := query.Session(&gorm.Session{DryRun: true}).Find(new([]T)).Statement
	var plan []byte
	row := stmt.ConnPool.QueryRowContext(stmt.Context, paginate.ExplainPrefix+stmt.SQL.String(), stmt.Vars...)
	if err := row.Scan(&plan); err != nil {
		return 0, err
	}
	return paginate.EstimateFromPlan(plan)
}

// tieBreakAllowed tells whether the tie-breaker may go into ORDER BY: Postgres rejects ORDER BY a
// column that is neither grouped nor in the SELECT DISTINCT list, so DISTINCT queries get no
// tie-breaker and GROUP BY ones only when they group by this very column.
func tieBreakAllowed(stmt *gorm.Statement, column string) bool {
	if stmt == nil || column == "" || stmt.Distinct || selectsDistinct(stmt) {
		return false
	}
	groupClause, ok := stmt.Clauses["GROUP BY"]
	if !ok {
		return true
	}
	groupBy, ok := groupClause.Expression.(clause.GroupBy)
	if !ok {
		return false
	}
	short := column[strings.LastIndex(column, ".")+1:]
	for _, groupColumn := range groupBy.Columns {
		// Group("templates.id, templates.title") is one raw clause column of several columns.
		for name := range strings.SplitSeq(groupColumn.Name, ",") {
			name = strings.TrimSpace(name)
			if groupColumn.Table != "" {
				name = groupColumn.Table + "." + name
			}
			// A qualified column must match exactly (other.id is not templates.id); an
			// unqualified one is taken as the same column when the name matches.
			if name == column || name == short {
				return true
			}
		}
	}
	return false
}

// selectsDistinct finds SELECT DISTINCT written as raw SQL: Select("DISTINCT ...").
func selectsDistinct(stmt *gorm.Statement) bool {
	for _, selected := range stmt.Selects {
		if startsWithDistinct(selected) {
			return true
		}
	}
	selectClause, ok := stmt.Clauses["SELECT"]
	if !ok {
		return false
	}
	switch expr := selectClause.Expression.(type) {
	case clause.Expr:
		return startsWithDistinct(expr.SQL)
	case clause.NamedExpr:
		return startsWithDistinct(expr.SQL)
	case clause.Select:
		return expr.Distinct
	}
	return false
}

func startsWithDistinct(sql string) bool {
	sql = strings.TrimSpace(sql)
	return len(sql) >= len("DISTINCT") && strings.EqualFold(sql[:len("DISTINCT")], "DISTINCT")
}
