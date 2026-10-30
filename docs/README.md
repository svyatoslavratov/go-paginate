# paginate documentation

Reference:

- [Configuration](configuration.md): fields, types, every `Config` setting, tags, the GORM builder, `Compile`.
- [Query syntax](query-syntax.md): every query parameter, how it is parsed, links.
- [Filters](filters.md): operators, `$not`, `$and`/`$or` chains, filter expressions, value types, caps.
- [Sorting and tie-breaker](sorting.md): key priority, defaults, `NULLS FIRST/LAST`, stable pages.
- [Pages and counts](pagination.md): page numbers or cursors (keyset pagination); exact, estimated or no counts.
- [Errors](errors.md): codes, fields, `HTTPStatus` and the JSON of a 400.
- [Security](security.md): what a client can and cannot do, and the knobs that change it.

nestjs-paginate:

- [Comparison with nestjs-paginate 16.0.1](nestjs-compatibility.md): feature by feature.
- [Migrating from nestjs-paginate](migrating-from-nestjs.md): every feature in TypeScript and in Go.

Use cases:

- [net/http (chi) + GORM](use-cases/chi-gorm.md): a list endpoint from handler to repository.
- [GORM: tags and the builder](use-cases/gorm-builder.md): configs from a model's tags, or from model and DTO fields.
- [squirrel + pgx, fasthttp](use-cases/squirrel-pgx.md): the SQL builder executor.
- [Joins, aliases, GROUP BY, subqueries](use-cases/joins-and-aliases.md): complex queries.
- [Exports and counts](use-cases/exports-and-counts.md): every row for the server, totals only for the client, big tables.
- [gRPC and other transports](use-cases/grpc.md): passing requests and meta between services.
- [OpenAPI](use-cases/openapi.md): documenting the parameters and the answer.
- [A custom executor](use-cases/custom-executor.md): `Endpoint.Prepare` and `NewResponse` on database/sql or any other library.
- [Testing a config](use-cases/testing.md): `Compile`, builder errors and SQL snapshots.

Performance and examples:

- [Benchmarks](benchmarks.md): the cost of parsing and `Prepare`, the library against hand-written code, end to end on Postgres.
- [The demo app](../examples/catalog): chi + GORM + Postgres in Docker, and the same page in the browser.
