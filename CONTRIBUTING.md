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
make lint      # golangci-lint with .golangci.yml
make vuln      # govulncheck
make cover     # coverage of the whole module
```

`make test-pg` runs the integration tests with `TEST_PG_DSN`; you can point it at your own
disposable database instead:

```sh
TEST_PG_DSN=postgres://postgres:test@127.0.0.1:5432/paginate?sslmode=disable go test ./...
```

Each integration test works in its own schema and drops it afterwards.

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

CI runs the tests on the two supported Go versions with Postgres, the linter, govulncheck and
CodeQL. A pull request is merged when CI is green and a maintainer has reviewed it.

By contributing you agree that your contribution is licensed under the [MIT License](LICENSE) of
the project, and that you follow the [code of conduct](CODE_OF_CONDUCT.md).
