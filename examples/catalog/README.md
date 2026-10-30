# Catalog: the go-paginate demo

A products table with everything go-paginate does: search and `searchBy`, every filter operator,
`$not` and `$or`, `filter=` expressions, typed values and enums, sorting by several keys, page
numbers and cursors, exact, estimated and no counts, `limit=0` and `limit=-1`, both answer formats.
For every answer the page shows the request, the SQL that ran and the meta.

**Live:** <https://svyatoslavratov.github.io/go-paginate/>. That build runs in the browser: the same
Go code compiled to WebAssembly, with Postgres compiled to WebAssembly too
([PGlite](https://pglite.dev)).

## Run it

```sh
cd examples/catalog
docker compose up --build
```

Then open <http://localhost:8080>. Postgres starts in a container, the server creates and fills the
table, and the page talks to the server. `PORT=9000 docker compose up --build` picks another port.
From the repository root, `make demo-up` does the same.

Against a Postgres of your own, without Docker:

```sh
cd examples
DATABASE_URL='postgres://postgres:postgres@localhost:5432/catalog?sslmode=disable' go run ./catalog/cmd/server
```

The browser build, from the repository root:

```sh
make demo
python3 -m http.server --directory _site 8080
```

## What is where

| Path | What |
|---|---|
| [catalog.go](catalog.go) | The model, whose `paginate` tags are the API of the list; the endpoints, compiled once; the fields for the page |
| [seed.sql](seed.sql) | The table and its 240 products, the same on every run |
| [cmd/server](cmd/server/main.go) | chi + GORM: `GET /api/products`, `GET /api/products/sql` (the same request in GORM's dry-run mode), `GET /api/schema` and the page |
| [cmd/wasm](cmd/wasm/main.go) | The browser build: `Endpoint.Prepare` and squirrel render the SQL, PGlite runs it, `paginate.NewResponse` builds the answer |
| [web](web) | The page: plain HTML, CSS and JavaScript, no build step |
| [Dockerfile](Dockerfile), [docker-compose.yml](docker-compose.yml) | The server image and the stack |

The API of the list is the tags of the model:

```go
type Product struct {
	ID       int     `json:"id" gorm:"primaryKey" paginate:"unique;sort;filter:$in,$gt,$gte,$lt,$lte,$btw"`
	Name     string  `json:"name" paginate:"sort;search;filter:$ilike,$sw,$in"`
	Category string  `json:"category" paginate:"sort;search;filter:$in;enum:lighting,seating,tables,storage,decor"`
	Price    float64 `json:"price" paginate:"sort;filter:$gt,$gte,$lt,$lte,$btw"`
	Tags     Tags    `json:"tags" gorm:"type:text[]" paginate:"filter:$contains;enum:sale,new,eco,bestseller,limited"`
	// ...
}
```

and the list endpoint is the whole integration:

```go
func listProducts(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := paginate.ParseRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		endpoint := catalog.Endpoint(catalog.OptionsOf(r.URL.Query()))
		page, err := gormx.Paginate[catalog.Product](r.Context(), *req, endpoint, db.Model(&catalog.Product{}))
		if err != nil {
			writeError(w, err) // paginate.HTTPStatus: 400 with code and field, or 500
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}
```

## Things to try

The chips under the filters fill in typical requests. A few worth reading closely:

- **OR across fields:** `filter=(category=$in:lighting,decor OR price=$lt:20) AND NOT name=$sw:Ember`.
- **OR within a field:** `filter.price=$lt:10&filter.price=$or:$gt:490`.
- **% is literal:** `search=%` finds the 14 products with "50% off" in the name, not all 240.
- **400: not a number:** `filter.price=$gt:abc` fails before any SQL runs: price is a `Number`.
  The answer has the code `invalid_value` and the field `price`, and the page marks that filter.
- **400: not in the enum:** `filter.category=$in:lighting,garden`; the enum is also an allowlist,
  and the value inputs suggest its values.
- **Cursor pages:** `pages=cursor` pages by keyset: next and previous only, no `OFFSET`. The SQL
  shows the `WHERE` that takes its place, with the range on the main key an index scan starts at
  (the fields of NOT NULL columns are tagged `notnull`).
- **No count, an estimate:** `count=none` reads a row more instead of counting; `count=estimate`
  shows the planner's estimate (`EXPLAIN`) for big results and counts small ones exactly (under
  100 rows).
- **Count only:** `limit=0` runs the count and skips the page query.
- **Sorting:** click a column, Shift+click to add a key. Products without a rating stay last in both
  directions (`NullsLast`), and the tie-breaker `id` keeps the pages stable.

Every state of the page is a link: the query string lives in the URL hash.

The page's `format=default`, `pages=cursor` and `count=estimate|none` parameters are the demo's
own: each combination is an endpoint compiled at startup. A real API picks one of them and keeps
it.
