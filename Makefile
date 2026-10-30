GOLANGCI_LINT_VERSION := v2.14.0
GOVULNCHECK_VERSION := v1.1.4
PG_CONTAINER := go-paginate-test-pg
PG_PORT := 55432
TEST_PG_DSN := postgres://postgres:test@127.0.0.1:$(PG_PORT)/paginate?sslmode=disable

.PHONY: test test-pg cover lint vuln tidy

## test: unit tests and examples
test:
	go test -race ./...

## test-pg: all tests, integration included, against a disposable Postgres in Docker
test-pg:
	docker run --rm -d --name $(PG_CONTAINER) -e POSTGRES_PASSWORD=test -e POSTGRES_DB=paginate -p 127.0.0.1:$(PG_PORT):5432 postgres:17-alpine
	until docker exec $(PG_CONTAINER) pg_isready -U postgres -d paginate >/dev/null 2>&1; do sleep 1; done
	TEST_PG_DSN='$(TEST_PG_DSN)' go test -race ./...; status=$$?; docker stop $(PG_CONTAINER) >/dev/null; exit $$status

## cover: coverage of the whole module (set TEST_PG_DSN for the integration tests)
cover:
	go test -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

## lint: golangci-lint with .golangci.yml
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

## vuln: known vulnerabilities in the code paths and dependencies
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

## tidy: go mod tidy
tidy:
	go mod tidy
