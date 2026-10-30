# Benchmarks

What the library costs per request, and how that compares with code written by hand for the same
request.

## The request

Every benchmark runs one typical list request, [`benchdata.Query`](../internal/benchdata/benchdata.go):

```http
GET /products?page=3&limit=20&sortBy=price:desc&sortBy=name:asc&search=red
    &filter.category=$in:lighting,decor&filter.price=$btw:10,500
    &filter.tags=$contains:sale&filter.discontinuedAt=$null
```

A page, two sort keys, a search over two columns and four filters. The "expression" variants add
`filter=(brand=$in:Lumen,Casa OR rating=$gte:4) AND NOT name=$sw:Old`.

"Hand-written" is what an endpoint without the library does for exactly this request
([handwritten.go](../internal/benchdata/handwritten.go)): it reads the same parameters, builds the
same SQL with the same bind parameters and the same tie-breaker, but knows only these four filters
and two sort keys, and checks no operators, fields or sizes. It is the floor: the cost of the SQL
itself, with none of the checks.

## Results

Apple M3 Pro, macOS, Go 1.26, Postgres 17 in Docker Desktop. The machine was busy with other work
while the benchmarks ran, so treat the times as rough. Allocations do not depend on the load and
are exact. The library and the hand-written code ran in turns, so their ratio is fair.

### The library alone

| Benchmark | Time | Allocations | Memory |
|---|---:|---:|---:|
| `ParseRequest`: the typical query string into a `Request` | 1.2 µs | 22 | 1.6 KiB |
| `Prepare`: an empty request | 1.4 µs | 36 | 1.9 KiB |
| `Prepare`: the typical request | 4.6 µs | 109 | 5.9 KiB |
| `Prepare`: the typical request with a `filter=` expression | 6.5 µs | 170 | 8.3 KiB |
| `Prepare`: a `filter=` expression of 99 nodes, just under the cap | 20 µs | 607 | 27.5 KiB |
| `Prepare`: `$in` with 1000 values, the cap | 32 µs | 1054 | 75.2 KiB |
| Parsing the expression of the typical request | 0.8 µs | 30 | 984 B |
| Parsing an expression with quoted values | 0.4 µs | 17 | 520 B |
| Parsing an expression of 99 nodes | 9.2 µs | 315 | 14.3 KiB |
| `NewResponse` and `json.Marshal` of 20 rows | 16.5 µs | 88 | 8.3 KiB |
| The same with `FormatNestJS`, links included | 21.7 µs | 94 | 10.6 KiB |

### With the library and by hand

Both sides start from the query string and end with the SQL or the rows; the library's side
includes `ParseRequest` and `Prepare`. The ratio is the median of ten runs in turns.

| What | Library | Hand-written | Library / hand-written |
|---|---|---|---:|
| squirrel: the count and the page as SQL ([`squirrelx.Statements`](../squirrelx)) | 17.2 µs, 297 allocations | 16.1 µs, 300 allocations | 0.95 |
| GORM: the count and the page in dry-run mode ([`gormx.Paginate`](../gormx)) | 15.1 µs, 323 allocations | 9.1 µs, 184 allocations | 1.87 |
| Postgres through GORM: 10 000 rows, count and page | 867 allocations | 717 allocations | 0.97 |
| Postgres through squirrel and pgx | 637 allocations | 572 allocations | 1.04 |

On Postgres one request took 4 to 25 ms on this machine, depending on its load, with the library
and without it alike.

### Reading the numbers

- What the library adds is parsing and checking the request: about 6 µs and 140 allocations for
  the typical request. That is the whole difference with GORM.
- With squirrel the difference disappears: squirrel's own rendering dominates, and the library
  hands it finished conditions instead of `squirrel.Eq` and `squirrel.ILike` values.
- On a database the library is noise: the query takes milliseconds, the library microseconds.
- The caps bound the worst case. The largest request the default caps admit, a 100-node expression
  or a list of 1000 values, takes tens of microseconds and less than 100 KiB.

## Running them

```sh
make bench      # everything but Postgres
make bench-pg   # with the end-to-end benchmarks on a disposable Postgres in Docker
```

`BenchmarkPostgres` runs only with `TEST_PG_DSN` set; it creates a products table of 10 000 rows in
its own schema. To compare two versions, run each with `-count 6` and feed both files to
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat), as
[CONTRIBUTING.md](../CONTRIBUTING.md#benchmarks) shows. On pull requests CI does it and posts the
comparison to the job summary.
