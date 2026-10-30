# Contributing

Thanks for helping. Bug reports, nestjs-paginate features that are missing, docs and code are all
welcome.

## Before you start

- For a bug, open an issue with the query string, the config and what you expected. For a security
  problem, follow [SECURITY.md](SECURITY.md) instead.
- For a larger change (a new operator, a new executor, a change of the answer), open an issue first,
  so the design is agreed before the code.

## Development

You need Go 1.26 or newer and Docker for the integration tests.

```sh
make test      # unit tests and examples
make test-pg   # everything, against a disposable Postgres 17 in Docker
make lint      # golangci-lint with .golangci.yml, the examples included
make vuln      # govulncheck
make cover     # coverage of the whole module
make bench     # benchmarks; make bench-pg adds the end-to-end ones on Postgres
make fuzz      # every fuzz target for 30s (FUZZ_TIME=5m for longer)
make demo-up   # the demo app on chi, GORM and Postgres at http://localhost:8080
make demo      # the browser build of the demo in _site
```

`make test-pg PG_IMAGE=postgres:13-alpine` runs the tests on another Postgres; CI runs 13 to 18.

`make test-pg` runs the integration tests with `TEST_PG_DSN`; you can point it at your own
disposable database instead:

```sh
TEST_PG_DSN=postgres://postgres:test@127.0.0.1:5432/paginate?sslmode=disable go test ./...
```

Each integration test works in its own schema and drops it afterwards.

### Fuzzing

The parsers take untrusted input, so each has a fuzz target in [fuzz_test.go](fuzz_test.go):
`filter=` expressions, `filter.<field>` values, `sortBy` and whole query strings. Besides "no
panic", the targets check what the parsers promise: every value ends up a bind parameter, and an
error is always `ErrInvalidRequest`. A failing input lands in `testdata/fuzz/<target>/`; commit it
with the fix, and `go test` replays it from then on. CI fuzzes every target for five minutes a week.

### Benchmarks

Benchmarks cover the parser, `Prepare`, the answer and both executors, each against hand-written
code for the same request ([internal/benchdata](internal/benchdata)). On a pull request CI runs
them on the base and the head and posts a [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
comparison to the job summary; it does not fail the build, since shared runners are noisy. Compare
locally the same way:

```sh
git stash && go test -run '^$' -bench . -count 6 ./... > old.txt
git stash pop && go test -run '^$' -bench . -count 6 ./... > new.txt
go run golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e old.txt new.txt
```

See [docs/benchmarks.md](docs/benchmarks.md) for the numbers.

## Changes

- Every new behavior has a table-driven test, including the error paths. SQL changes get a
  snapshot test (`squirrelx.Statements`, the GORM dry-run tests) and, where types matter, an
  integration test on Postgres.
- Keep the query syntax compatible with nestjs-paginate. Where this package differs, document it in
  [docs/nestjs-compatibility.md](docs/nestjs-compatibility.md).
- The root package has no dependencies; anything that needs one goes into an executor package.
- Exported identifiers have doc comments; comments are in English.
- Add a line to the "Unreleased" section of [CHANGELOG.md](CHANGELOG.md).
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat: …`, `fix: …`, `docs: …`.

## Pull requests

CI runs the tests on the two supported Go versions and on Postgres 13 to 18, the linter,
govulncheck, CodeQL, the examples with the demo in Docker, and
[gorelease](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease), which fails on an incompatible
change of the API unless the pull request has the `breaking-change` label. A pull request is merged
when CI is green and a maintainer has reviewed it.

Labels sort the pull request into the release notes ([.github/release.yml](.github/release.yml)):
`breaking-change`, `security`, `enhancement`, `bug`, `documentation`, `dependencies`;
`skip-changelog` leaves it out.

By contributing you agree that your contribution is licensed under the [MIT License](LICENSE) of
the project, and that you follow the [code of conduct](CODE_OF_CONDUCT.md).
