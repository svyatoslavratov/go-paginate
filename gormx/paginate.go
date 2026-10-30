// Package gormx runs paginate requests on GORM and builds paginate configs from GORM models.
package gormx

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/svyatoslavratov/go-paginate"
)

// Paginate runs a paginated query on top of db: db carries the table, joins, Select, preloads and
// the conditions of the endpoint; the request adds search, filters, sorting and the page. T is the
// type Find scans rows into. Errors wrap paginate.ErrInvalidRequest for the client's mistakes.
func Paginate[T any](ctx context.Context, req paginate.Request, cfg paginate.Config, db *gorm.DB) (*paginate.Response[T], error) {
	if db == nil {
		return nil, errors.New("gormx: db is required")
	}
	q, err := paginate.Prepare(&req, cfg)
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

	var count int64
	if err := query.Session(&gorm.Session{}).Count(&count).Error; err != nil {
		return nil, q.WrapError("count", err)
	}
	if q.Mode == paginate.ModeCount {
		return paginate.NewResponse[T](nil, req, uint64(count)), nil
	}

	if q.Mode == paginate.ModeDefault {
		query = query.Limit(int(q.Limit)).Offset(int(q.Offset))
	}
	for _, order := range q.OrderBy(tieBreakAllowed(query.Statement, q.TieBreak)) {
		query = query.Order(order)
	}

	var data []T
	if err := query.Find(&data).Error; err != nil {
		return nil, q.WrapError("select", err)
	}
	return paginate.NewResponse(data, req, uint64(count)), nil
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
		name := groupColumn.Name
		if groupColumn.Table != "" {
			name = groupColumn.Table + "." + name
		}
		// A qualified column must match exactly (other.id is not templates.id); an unqualified one
		// is taken as the same column when the name matches.
		if name == column || name == short {
			return true
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
