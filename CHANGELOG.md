# Changelog

All notable changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until v1, a minor release may change
the API; the changelog says how to upgrade.

## [Unreleased]

The first release, to be tagged v0.1.0.

### Added

- The query language of nestjs-paginate: `page`, `limit` (`0` for the count only, `-1` for every
  row where the config allows it), repeated `sortBy=field:asc|desc`, `search`, `searchBy`,
  `filter.<field>=[$or:|$and:][$not:]$operator:value` with `$eq`, `$gt`, `$gte`, `$lt`, `$lte`,
  `$in`, `$btw`, `$null`, `$ilike`, `$sw`, `$contains`, `filter=<expression>` with `AND`, `OR`,
  `NOT`, parentheses and quoted values, `withDeleted`, and `paginationMode`.
- `paginate.ParseRequest` (net/http), `paginate.ParseQuery`, `fasthttpx.ParseRequest`.
- `paginate.Prepare`: checks a request against a `Config` and renders SQL conditions and
  `ORDER BY` for any executor.
- Executors: `gormx.Paginate` and `squirrelx.Paginate` (pgx), with
  `squirrelx.Statements` to see the SQL.
- `gormx.NewConfigBuilder`: configs from model and DTO fields, embedded and
  `gorm:"embedded"` structs included.
- Answers in this package's shape or in nestjs-paginate's (`FormatNestJS`), with links to the
  neighboring pages; `Meta.NestJS()` and `Meta.Links()`.
- A tie-breaker column for stable pages, `NULLS FIRST/LAST`, multi-word search, typed comparison
  (`CompareAsText` to opt out), caps on search length, filter values, lists and expression size,
  and `Config.Validate`.
- [Comparison with nestjs-paginate 16.0.1](docs/nestjs-compatibility.md) and a
  [migration guide](docs/migrating-from-nestjs.md).

[Unreleased]: https://github.com/svyatoslavratov/go-paginate/commits/main
