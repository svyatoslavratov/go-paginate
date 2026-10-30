package squirrelx_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/squirrelx"
)

type Account struct {
	ID      int64  `db:"id"`
	Number  string `db:"number"`
	Balance int64  `db:"balance"`
}

func accountsConfig() paginate.Config {
	cfg := paginate.NewConfig("a", []string{"a.created_at", "a.balance"}, map[string]string{
		"number":  "a.number",
		"balance": "a.balance",
		"owner":   "o.name",
	})
	cfg.SearchableColumns = []string{"a.number", "o.name"}
	cfg.FilterableColumns = paginate.FilterableColumns{"a.balance": {paginate.OpGte}}
	cfg.TieBreakColumn = "a.id"
	return *cfg
}

// Paginate runs the count and the page on a pgx pool.
func ExamplePaginate() {
	var pool *pgxpool.Pool // opened elsewhere
	req, _ := paginate.ParseRequest(httptest.NewRequest(http.MethodGet, "/accounts?filter.balance=$gte:100", nil))

	qb := squirrel.Select("a.id", "a.number", "a.balance").
		From("accounts AS a").
		Join("owners AS o ON o.id = a.owner_id").
		Where(squirrel.Eq{"a.owner_id": 7})
	scan := func(rows pgx.Rows) ([]Account, error) {
		return pgx.CollectRows(rows, pgx.RowToStructByName[Account])
	}
	if pool != nil {
		page, err := squirrelx.Paginate(context.Background(), *req, accountsConfig(), pool, scan, qb)
		_, _ = page, err
	}
}

// Statements shows what a query string turns into; Paginate runs the same statements.
func ExampleStatements() {
	req, _ := paginate.ParseRequest(httptest.NewRequest(http.MethodGet, "/accounts?page=2&limit=10&sortBy=balance:desc&search=ann&filter.balance=$gte:100", nil))
	qb := squirrel.Select("a.id", "a.number", "a.balance").
		From("accounts AS a").
		Join("owners AS o ON o.id = a.owner_id").
		Where(squirrel.Eq{"a.owner_id": 7})

	count, page, err := squirrelx.Statements(qb, req, accountsConfig())
	if err != nil {
		panic(err)
	}
	fmt.Println(count.SQL)
	fmt.Println(page.SQL)
	fmt.Println(page.Args...)
	// Output:
	// SELECT COUNT(*) FROM (SELECT a.id, a.number, a.balance FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id WHERE a.owner_id = $1 AND ((CAST(a.number AS TEXT) ILIKE $2) OR (CAST(o.name AS TEXT) ILIKE $3)) AND a.balance >= $4) AS paginate_count
	// SELECT a.id, a.number, a.balance FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id WHERE a.owner_id = $1 AND ((CAST(a.number AS TEXT) ILIKE $2) OR (CAST(o.name AS TEXT) ILIKE $3)) AND a.balance >= $4 ORDER BY a.balance desc, a.id asc LIMIT 10 OFFSET 10
	// 7 %ann% %ann% 100
}
