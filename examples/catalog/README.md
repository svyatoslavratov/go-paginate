# Catalog: the go-paginate demo

A products table with everything go-paginate does: search and `searchBy`, every filter operator,
`$not` and `$or`, `filter=` expressions, sorting by several keys, `limit=0` and `limit=-1`, both
answer formats. For every answer the page shows the request, the SQL and the meta.

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
| [catalog.go](catalog.go) | The model, the `paginate.Config` of the endpoint, the fields for the page |
| [seed.sql](seed.sql) | The table and its 240 products, the same on every run |
| [cmd/server](cmd/server/main.go) | chi + GORM: `GET /api/products`, `GET /api/products/sql` (the same request in GORM's dry-run mode), `GET /api/schema` and the page |
| [cmd/wasm](cmd/wasm/main.go) | The browser build: `paginate.Prepare` and squirrel render the SQL, PGlite runs it, `paginate.NewResponse` builds the answer |
| [web](web) | The page: plain HTML, CSS and JavaScript, no build step |
| [Dockerfile](Dockerfile), [docker-compose.yml](docker-compose.yml) | The server image and the stack |

The list endpoint is the whole integration:

```go
func listProducts(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := paginate.ParseRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		page, err := gormx.Paginate[catalog.Product](r.Context(), *req, catalog.Config(), db.Model(&catalog.Product{}))
		if err != nil {
			writeError(w, err) // 400 for paginate.ErrInvalidRequest, 500 for the rest
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
- **400: not a number:** `filter.price=$gt:abc` reaches Postgres as a bind parameter. Postgres
  rejects it with SQLSTATE 22P02, and the client gets a 400, not a 500.
- **Count only:** `limit=0` runs the count and skips the page query.
- **Sorting:** click a column, Shift+click to add a key. Products without a rating stay last in both
  directions (`NullsLast`), and the tie-breaker `id` keeps the pages stable.

Every state of the page is a link: the query string lives in the URL hash.

The page's `format=default` parameter is the demo's own: it switches the config to go-paginate's
default answer, which has no links.
