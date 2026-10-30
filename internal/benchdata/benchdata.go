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
	cfg := paginate.NewConfig("products",
		[]string{"products.id", "products.name", "products.price", "products.rating", "products.released_at"},
		map[string]string{
			"id":             "products.id",
			"name":           "products.name",
			"brand":          "products.brand",
			"category":       "products.category",
			"price":          "products.price",
			"rating":         "products.rating",
			"stock":          "products.stock",
			"tags":           "products.tags",
			"releasedAt":     "products.released_at",
			"discontinuedAt": "products.discontinued_at",
		})
	cfg.SearchableColumns = []string{"products.name", "products.brand"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"products.id":              {paginate.OpIn},
		"products.name":            paginate.AllOperators,
		"products.brand":           {paginate.OpIn, paginate.OpSw},
		"products.category":        {paginate.OpIn},
		"products.price":           {paginate.OpBtw, paginate.OpGte, paginate.OpLte},
		"products.rating":          {paginate.OpGte, paginate.OpBtw},
		"products.stock":           {paginate.OpGt, paginate.OpLt},
		"products.tags":            {paginate.OpContains},
		"products.released_at":     {paginate.OpBtw},
		"products.discontinued_at": {paginate.OpNull},
	}
	cfg.TieBreakColumn = "products.id"
	return *cfg
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
