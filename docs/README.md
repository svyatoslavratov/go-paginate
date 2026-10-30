# paginate documentation

Reference:

- [Query syntax](query-syntax.md): every query parameter, how it is parsed, links.
- [Filters](filters.md): operators, `$not`, `$and`/`$or` chains, filter expressions, value types, caps.
- [Sorting and tie-breaker](sorting.md): key priority, defaults, `NULLS FIRST/LAST`, stable pages.
- [Configuration](configuration.md): every `Config` field, defaults, `Validate`.
- [Security](security.md): what a client can and cannot do, and the knobs that change it.

nestjs-paginate:

- [Comparison with nestjs-paginate 16.0.1](nestjs-compatibility.md): feature by feature.
- [Migrating from nestjs-paginate](migrating-from-nestjs.md): every feature in TypeScript and in Go.

Use cases:

- [net/http (chi) + GORM](use-cases/chi-gorm.md): a list endpoint from handler to repository.
- [GORM builder](use-cases/gorm-builder.md): configs from model and DTO fields, embedded base models.
- [squirrel + pgx, fasthttp](use-cases/squirrel-pgx.md): the SQL builder executor.
- [Joins, aliases, GROUP BY, subqueries](use-cases/joins-and-aliases.md): complex queries.
- [Exports and counts](use-cases/exports-and-counts.md): every row for the server, totals only for the client.
- [gRPC and other transports](use-cases/grpc.md): passing requests and meta between services.
- [OpenAPI](use-cases/openapi.md): documenting the parameters and the answer.
- [A custom executor](use-cases/custom-executor.md): `paginate.Prepare` on database/sql or any other library.
- [Testing a config](use-cases/testing.md): `Validate`, builder errors and SQL snapshots.

Performance and examples:

- [Benchmarks](benchmarks.md): the cost of parsing and `Prepare`, the library against hand-written code, end to end on Postgres.
- [The demo app](../examples/catalog): chi + GORM + Postgres in Docker, and the same page in the browser.
