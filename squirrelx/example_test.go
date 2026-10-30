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

// accounts is the endpoint of the examples; o.name comes from a joined table.
var accounts = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "number", Column: "a.number", Searchable: true},
		{Name: "balance", Column: "a.balance", Type: paginate.Int, Sortable: true, Filter: []paginate.Operator{paginate.OpGte}},
		{Name: "createdAt", Column: "a.created_at", Type: paginate.Time, Sortable: true},
		{Name: "owner", Column: "o.name", Searchable: true},
	},
	TieBreak: "a.id",
})

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
		page, err := squirrelx.Paginate(context.Background(), *req, accounts, pool, scan, qb)
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

	plan, err := squirrelx.Statements(qb, req, accounts)
	if err != nil {
		panic(err)
	}
	fmt.Println(plan.Count.SQL)
	fmt.Println(plan.Page.SQL)
	fmt.Println(plan.Page.Args...)
	// Output:
	// SELECT COUNT(*) FROM (SELECT a.id, a.number, a.balance FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id WHERE a.owner_id = $1 AND ((CAST(a.number AS TEXT) ILIKE $2) OR (CAST(o.name AS TEXT) ILIKE $3)) AND a.balance >= $4) AS paginate_count
	// SELECT a.id, a.number, a.balance FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id WHERE a.owner_id = $1 AND ((CAST(a.number AS TEXT) ILIKE $2) OR (CAST(o.name AS TEXT) ILIKE $3)) AND a.balance >= $4 ORDER BY a.balance DESC, a.id ASC LIMIT 10 OFFSET 10
	// 7 %ann% %ann% 100
}
