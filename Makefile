GOLANGCI_LINT_VERSION := v2.14.0
GOVULNCHECK_VERSION := v1.9.0
PG_CONTAINER := go-paginate-test-pg
PG_PORT := 55432
PG_IMAGE ?= postgres:17-alpine
TEST_PG_DSN := postgres://postgres:test@127.0.0.1:$(PG_PORT)/paginate?sslmode=disable
FUZZ_TIME ?= 30s
FUZZ_TARGETS := FuzzParseFilterExpression FuzzParseFilterToken FuzzParseSortBy FuzzParseQuery FuzzCursor

.PHONY: test test-pg cover bench bench-pg fuzz lint vuln tidy demo demo-up

# with-pg runs $(1) against a disposable Postgres in Docker (PG_IMAGE) and removes it afterwards.
define with-pg
	docker run --rm -d --name $(PG_CONTAINER) -e POSTGRES_PASSWORD=test -e POSTGRES_DB=paginate -p 127.0.0.1:$(PG_PORT):5432 $(PG_IMAGE)
	until docker exec $(PG_CONTAINER) pg_isready -h 127.0.0.1 -U postgres -d paginate >/dev/null 2>&1; do sleep 1; done
	TEST_PG_DSN='$(TEST_PG_DSN)' $(1); status=$$?; docker stop $(PG_CONTAINER) >/dev/null; exit $$status
endef

## test: unit tests and examples
test:
	go test -race ./...

## test-pg: all tests, integration included, on Postgres in Docker; PG_IMAGE=postgres:13-alpine for another version
test-pg:
	$(call with-pg,go test -race -count=1 ./...)

## cover: coverage of the whole module (set TEST_PG_DSN for the integration tests)
cover:
	go test -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

## bench: benchmarks of the parsers, Prepare and the executors, with the library and without
bench:
	go test -run '^$$' -bench . -benchmem ./...

## bench-pg: the benchmarks plus the end-to-end ones on Postgres in Docker
bench-pg:
	$(call with-pg,go test -run '^$$' -bench . -benchmem ./...)

## fuzz: every fuzz target for FUZZ_TIME (30s)
fuzz:
	for target in $(FUZZ_TARGETS); do go test -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZ_TIME) . || exit 1; done

## lint: golangci-lint with .golangci.yml, the examples included
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...
	cd examples && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

## vuln: known vulnerabilities in the code paths and dependencies, the examples included
# govulncheck runs on the module's toolchain: built with an older default Go, it cannot load the code.
vuln:
	GOTOOLCHAIN=$$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	cd examples && GOTOOLCHAIN=$$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

## tidy: go mod tidy, the examples included
tidy:
	go mod tidy
	cd examples && go mod tidy

## demo: the browser demo (Go WebAssembly and PGlite) in _site; serve it with any static server
demo:
	rm -rf _site && mkdir -p _site
	sed 's/name="demo-engine" content="server"/name="demo-engine" content="browser"/' examples/catalog/web/index.html > _site/index.html
	grep -q 'content="browser"' _site/index.html
	cp examples/catalog/web/style.css examples/catalog/web/app.js examples/catalog/web/engine.js _site/
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" _site/
	cd examples && GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o ../_site/demo.wasm ./catalog/cmd/wasm

## demo-up: the demo on chi, GORM and Postgres in Docker, at http://localhost:8080
demo-up:
	docker compose -f examples/catalog/docker-compose.yml up --build
