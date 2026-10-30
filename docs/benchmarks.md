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
while the benchmarks ran, so the times are scaled to a quiet run by the parser benchmarks, whose
code did not change between the two: treat them as rough. Allocations do not depend on the load and
are exact. The library and the hand-written code ran in turns, so their ratio is fair.

### The library alone

`Prepare` runs on a compiled `Endpoint`: the config was checked once, at `Compile`.

| Benchmark | Time | Allocations | Memory |
|---|---:|---:|---:|
| `ParseRequest`: the typical query string into a `Request` | 1.3 µs | 22 | 1.3 KiB |
| `Prepare`: an empty request | 0.3 µs | 9 | 632 B |
| `Prepare`: the typical request | 2.8 µs | 77 | 4.0 KiB |
| `Prepare`: the typical request with a `filter=` expression | 5 µs | 138 | 6.3 KiB |
| `Prepare`: a `filter=` expression of 99 nodes, just under the cap | 20 µs | 580 | 26.2 KiB |
| `Prepare`: `$in` with 1000 integers, the cap, each checked against the field's type | 46 µs | 1024 | 73.6 KiB |
| Parsing the expression of the typical request | 0.8 µs | 30 | 984 B |
| Parsing an expression with quoted values | 0.4 µs | 17 | 520 B |
| Parsing an expression of 99 nodes | 9.2 µs | 315 | 14.3 KiB |
| `NewResponse` and `json.Marshal` of 20 rows | 19 µs | 92 | 9.0 KiB |
| The same with `FormatNestJS`, links included | 22 µs | 103 | 11.3 KiB |

### With the library and by hand

Both sides start from the query string and end with the SQL or the rows; the library's side
includes `ParseRequest` and `Prepare`. The ratio is the median of runs in turns.

| What | Library | Hand-written | Library / hand-written |
|---|---|---|---:|
| squirrel: the count and the page as SQL ([`squirrelx.Statements`](../squirrelx)) | 17 µs, 265 allocations | 19 µs, 300 allocations | 0.87 |
| GORM: the count and the page in dry-run mode ([`gormx.Paginate`](../gormx)) | 18 µs, 304 allocations | 11 µs, 184 allocations | 1.64 |
| Postgres through GORM: 10 000 rows, count and page | 845 allocations | 717 allocations | 1.01 |
| Postgres through squirrel and pgx | 608 allocations | 572 allocations | 1.04 |

On Postgres one request took 4 to 25 ms on this machine, depending on its load, with the library
and without it alike; the ratios of the Postgres rows are the medians of paired runs.

### Reading the numbers

- What the library adds is parsing and checking the request: a few microseconds and about 100
  allocations for the typical request. That is the whole difference with GORM.
- With squirrel the library is cheaper than the hand-written code: squirrel's own rendering
  dominates, and the library hands it finished conditions instead of `squirrel.Eq` and
  `squirrel.ILike` values.
- On a database the library is noise: the query takes milliseconds, the library microseconds.
- Checking values against types costs little: a list of 1000 integers is the worst case.
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
