// Package benchdata holds what the benchmarks share: a typical list request, the config it runs
// against and a products table to run it on.
package benchdata

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/svyatoslavratov/go-paginate"
)

// Query is a typical list request: a page, two sort keys, a search and four filters.
const Query = "page=3&limit=20&sortBy=price:desc&sortBy=name:asc&search=red" +
	"&filter.category=$in:lighting,decor&filter.price=$btw:10,500" +
	"&filter.tags=$contains:sale&filter.discontinuedAt=$null"

// Expression is a filter expression added to Query in the "expression" benchmarks.
const Expression = "(brand=$in:Lumen,Casa OR rating=$gte:4) AND NOT name=$sw:Old"

// ExpressionQuery is Query with Expression.
var ExpressionQuery = Query + "&filter=" + url.QueryEscape(Expression)

// Columns are the columns the benchmarks select.
var Columns = []string{
	"products.id", "products.name", "products.brand", "products.category",
	"products.price", "products.rating", "products.stock", "products.released_at",
}

// Config is the config of the products endpoint.
func Config() paginate.Config {
	return paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "products.id", Type: paginate.Int, Sortable: true, Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "name", Column: "products.name", Type: paginate.Text, Sortable: true, Searchable: true, Filter: paginate.AllOperators},
			{Name: "brand", Column: "products.brand", Type: paginate.Text, Searchable: true, Filter: []paginate.Operator{paginate.OpIn, paginate.OpSw}},
			{Name: "category", Column: "products.category", Type: paginate.Text, Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "price", Column: "products.price", Type: paginate.Number, Sortable: true, Filter: []paginate.Operator{paginate.OpBtw, paginate.OpGte, paginate.OpLte}},
			{Name: "rating", Column: "products.rating", Type: paginate.Number, Sortable: true, Filter: []paginate.Operator{paginate.OpGte, paginate.OpBtw}},
			{Name: "stock", Column: "products.stock", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpGt, paginate.OpLt}},
			{Name: "tags", Column: "products.tags", Type: paginate.ArrayOf(paginate.Text), Filter: []paginate.Operator{paginate.OpContains}},
			{Name: "releasedAt", Column: "products.released_at", Type: paginate.Time, Sortable: true, Filter: []paginate.Operator{paginate.OpBtw}},
			{Name: "discontinuedAt", Column: "products.discontinued_at", Type: paginate.Time, Filter: []paginate.Operator{paginate.OpNull}},
		},
		TieBreak: "products.id",
	}
}

// Endpoint is the compiled products endpoint.
func Endpoint() *paginate.Endpoint {
	return paginate.MustCompile(Config())
}

// ProductsSQL creates the products table with the given number of generated rows, the same on every
// run: names, brands, categories, prices, ratings (some NULL), tags and dates.
func ProductsSQL(rows int) string {
	return strings.Replace(productsSQL, "$rows", strconv.Itoa(rows), 1)
}

const productsSQL = `
CREATE TABLE products (
	id              bigint PRIMARY KEY,
	name            text NOT NULL,
	brand           text NOT NULL,
	category        text NOT NULL,
	price           numeric(10, 2) NOT NULL,
	rating          numeric(2, 1),
	stock           integer NOT NULL,
	tags            text[] NOT NULL,
	released_at     timestamptz NOT NULL,
	discontinued_at timestamptz
);
INSERT INTO products
SELECT i,
	(ARRAY['Red','Blue','Oak','Old','Smart','Mini'])[1 + i % 6] || ' ' ||
		(ARRAY['lamp','chair','desk','sofa','shelf','rug'])[1 + (i / 6) % 6] || ' ' || i,
	(ARRAY['Nordwood','Lumen','Casa','Ferro','Oakline'])[1 + i % 5],
	(ARRAY['lighting','seating','tables','storage','decor'])[1 + (i / 3) % 5],
	round((5 + (i * 37) % 495)::numeric + (i % 100) / 100.0, 2),
	CASE WHEN i % 9 = 0 THEN NULL ELSE round(1 + ((i * 13) % 40) / 10.0, 1) END,
	(i * 17) % 120,
	CASE WHEN i % 4 = 0 THEN ARRAY['sale', 'new'] WHEN i % 4 = 1 THEN ARRAY['eco'] ELSE ARRAY['sale'] END,
	timestamptz '2023-01-01' + i * interval '1 hour',
	CASE WHEN i % 13 = 0 THEN timestamptz '2025-06-01' END
FROM generate_series(1, $rows) AS g(i);
CREATE INDEX products_category ON products (category);
CREATE INDEX products_price ON products (price);
ANALYZE products;
`
