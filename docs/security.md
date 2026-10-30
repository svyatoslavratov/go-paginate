# Security

A paginated endpoint hands part of the SQL to the client. This page lists what the client controls,
what it cannot touch, and the settings that change the balance.

## SQL injection

- Column names, table names and `ORDER BY` expressions come only from the config. A client names
  API fields, and only fields in `FieldColumns` reach SQL. Others are a 400 with
  `StrictValidate` and dropped without it.
- The sort direction is checked to be `asc` or `desc`, also for requests built in code, because it
  goes into `ORDER BY` as text.
- Every filter and search value is a bind parameter (`$1`, `$2`, ...), including `$in` lists, which
  get one placeholder per value, and `$contains` arrays, which travel as one parameter.

Keep config columns out of client reach: never build `FieldColumns`,
`SortableColumns` or `FilterableColumns` from request data.

## Data exposure

- Clients filter, search and sort only by columns the config lists. Expose the columns you would
  show anyway. A filter on a hidden column (`filter.passwordHash=$sw:a`) leaks its content one
  character at a time, even if the column is never returned.
- Scoping rows to a tenant or user is the base query's job (`qb.Where("tenant_id = ?", id)`). The
  request's filters are ANDed with it and cannot widen it.
- `withDeleted` is ignored unless `AllowWithDeletedInQuery` is set.

## Resource limits

- `limit` is capped at `MaxLimit` (100).
- `paginationMode=disabled` and `limit=-1` (every row) are ignored unless `AllowDisabledPagination`
  is set; the client gets a regular page. Before this setting existed, any client could fetch a
  whole table in one request. Server code that needs every row sets
  `Request.Mode` itself, see [exports](use-cases/exports-and-counts.md).
- A page whose offset overflows a 64-bit integer is a 400. Large offsets are still expensive:
  Postgres reads and drops every skipped row. If deep pages matter, cap `page` in the handler.
- `search` is capped at `MaxSearchLength` characters (256), filter values per request at
  `MaxFilterConditions` (100), list values at `MaxFilterValues` (1000).
- A `filter=` expression is parsed recursively; it is capped at `MaxFilterExpressionComplexity`
  nodes (100), counted while parsing, so deep nesting is refused before it can exhaust the stack.
  Its terms are always checked against `FilterableColumns`, with or without `StrictValidate`.
- `%`, `_` and `\` in `search`, `$ilike` and `$sw` are literal, so a client cannot send expensive
  wildcard patterns. Substring search (`ILIKE '%term%'`) still scans the table: keep searchable
  columns few, or back them with a trigram index (`CREATE INDEX ... USING gin (name gin_trgm_ops)`).
- `$eq`, `$in` and range operators compare with the column's type and can use indexes. With
  `CompareAsText` they cast the column to text and cannot.

## Links

Links in the answer are built from the request path and the config, never from the Host header:
a client controls that header, and a link with its host could reach other users through a shared
cache. Set `LinksBaseURL` for absolute links. Values in links are URL-encoded.

## Errors

- `ErrInvalidRequest` messages are written for the client. They name the API field or the
  column and the operator, and for type errors they carry the Postgres message (for example
  `invalid input syntax for type uuid: "x"`).
- `ErrInvalidConfig` and wrapped database errors are for logs. Answer them with a generic 500.

## Checklist for an endpoint

- The base query scopes rows to the caller.
- Only displayable columns are sortable, searchable or filterable.
- `TieBreakColumn` is set, or the builder set it.
- `AllowDisabledPagination` and `AllowWithDeletedInQuery` are set only where clients need them.
- A unit test calls `config.Validate()`.
