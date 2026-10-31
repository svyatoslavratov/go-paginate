# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until v1, a minor release may change
the API; the changelog says how to upgrade.

## [Unreleased]

## [0.1.0] - 2026-10-31

The first release.

### Added

- The query language of nestjs-paginate: `page`, `limit` (`0` for the count only, `-1` for every
  row where the config allows it), repeated `sortBy=field:asc|desc`, `search`, `searchBy`,
  `filter.<field>=[$or:|$and:][$not:]$operator:value` with `$eq`, `$gt`, `$gte`, `$lt`, `$lte`,
  `$in`, `$btw`, `$null`, `$ilike`, `$sw`, `$contains`, `filter=<expression>` with `AND`, `OR`,
  `NOT`, parentheses and quoted values, `cursor`, `withDeleted`, and `paginationMode`.
- Field-centric configs: `Config.Fields` lists each API field once, with its column, its `Type`
  and what clients may do with it.
- `paginate.Compile` and `MustCompile`: a config is checked once, with every problem reported, and
  becomes an `Endpoint`; `Endpoint.Prepare` checks a request and renders SQL for any executor.
- Field types: `Text`, `Int`, `Number`, `Bool`, `Time`, `Date`, `UUID`, `Enum`, `ArrayOf`, and
  `TypeOf` for Go types. Values that do not fit are a 400 naming the field, before the query runs.
- Struct tags: `paginate.ConfigOf` and `gormx.ConfigOf` read
  `paginate:"sort;search;filter:$in;type:uuid;enum:a,b;column:x;name:y;unique;notnull"`, and
  `Config.Field` finds a field by name, so code changes what the tags cannot describe.
- `gormx.NewBuilder`: fields from pointers to model and DTO fields, embedded and
  `gorm:"embedded"` structs included.
- Cursor (keyset) pagination: `Pagination: CursorPagination`, with nestjs-paginate's `cursor`
  parameter, meta and links, a `WHERE` that starts with a range an index scan can start at
  (`Field.NotNull`, tag `notnull`), NULL handling in both directions, and cursors tied to their
  sort.
- Count modes: `CountExact`, `CountEstimate` (the planner's estimate, exact below
  `ExactCountBelow`), `CountNone` (a next page flag from one extra row), and `ParallelCount`.
- Structured errors: `*Error` with a `Code` and the `Field` it is about, its JSON ready for a 400,
  and `HTTPStatus`.
- Executors: `gormx.Paginate` and `squirrelx.Paginate` (pgx), with `squirrelx.Statements` to see
  the SQL.
- Answers in this package's shape or in nestjs-paginate's (`FormatNestJS`), both with
  `meta.sortBy` as `[field, "ASC"|"DESC"]` pairs, with links to the neighboring pages,
  `Meta.NestJS()`, `Meta.Links()`, and `paginate.Map` for answering with other types.
- A tie-breaker column for stable pages, `NULLS FIRST/LAST`, multi-word search, typed comparison
  (`CompareAsText` to opt out), `Lenient` for nestjs-paginate's tolerance of unknown fields, and
  caps on search length, filter values, lists and expression size.
- [Comparison with nestjs-paginate 16.0.1](docs/nestjs-compatibility.md) and a
  [migration guide](docs/migrating-from-nestjs.md).
- The demo app in [examples/catalog](examples/catalog): chi, GORM and Postgres with
  `docker compose up`, and the same page on GitHub Pages, in the browser (Go WebAssembly and PGlite).
- Fuzz tests of the parsers and cursors, benchmarks against hand-written code, and CI on Postgres
  13 to 18 with gorelease checks of the API.

[Unreleased]: https://github.com/svyatoslavratov/go-paginate/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/svyatoslavratov/go-paginate/releases/tag/v0.1.0
