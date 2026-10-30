# Security

A paginated endpoint hands part of the SQL to the client. This page lists what the client controls,
what it cannot touch, and the settings that change the balance.

## SQL injection

- Column names, table names and `ORDER BY` expressions come only from the config. A client names
  API fields, and only the `Fields` of the endpoint reach SQL. Other names are a 400, or dropped
  with `Lenient`.
- The sort direction is checked to be `ASC` or `DESC`, also for requests built in code, because it
  goes into `ORDER BY` as text.
- Every filter, search and cursor value is a bind parameter (`$1`, `$2`, ...), including `$in`
  lists, which get one placeholder per value, and `$contains` arrays, which travel as one
  parameter.

Keep config columns out of client reach: never build a `Field` or the `TieBreak` from request
data.

## Strict or Lenient

An endpoint is strict unless its config sets `Lenient`: a field, an operator or a `searchBy` the
endpoint does not allow is a 400 that names it (`unknown_field`, `filter_not_allowed`,
`operator_not_allowed`, `sort_not_allowed`, `search_not_allowed`). A typo in a client fails at
once instead of returning rows the client did not filter.

`Lenient` drops them instead, the way nestjs-paginate does. A dropped filter returns more rows
than the client asked for, so never rely on a client filter to hide rows: that is the base
query's job. Malformed values, values that do not fit the type, values over the caps, filter
expressions and cursors fail either way.

## Types

A field's `Type` checks filter values before the query runs: a value that does not fit is a 400
that names the field, and no query runs. `Enum` is an allowlist: `filter.status=$in:active,deleted`
is a 400 when `deleted` is not one of its values. `Compile` refuses `$contains` on a field whose
type is not an array, which the database would reject with an error the client gets as a 500.

A field without a type leaves the check to the database. A value of the wrong type then costs a
round trip, and the 400 carries the database's message instead of the field. See
[filters](filters.md#values-and-types).

## Cursors

A cursor is base64url-encoded JSON: the direction, the sort it belongs to, and the values of the
sort keys and the tie-breaker of the row it marks:

```json
["n","createdAt:DESC,id:ASC","2024-05-02T10:00:00Z",7]
```

It is opaque, not secret: it is neither encrypted nor signed, so anyone can read those values and
write a cursor of their own. `Prepare` checks every cursor before it uses one:

- at most 4096 bytes, valid base64url, a JSON array of the right length;
- made for the sort of the request: a cursor sent with another `sortBy` is rejected, not misread;
- every value fits the type of its field (an `Enum` takes any value here); a field without a type
  leaves the check to the database, as for filters.

A cursor that fails is a 400 with `invalid_cursor`, `Lenient` or not. Its values are bind
parameters, so a forged cursor is at most a range condition on the sort keys, which sorting
reveals anyway. Since a cursor shows the sort values of a row, sort only by fields clients may
see.

## Data exposure

- Clients filter, search and sort only by the fields the config lists. Expose the fields you would
  show anyway. A filter on a hidden column (`filter.passwordHash=$sw:a`) leaks its content one
  character at a time, even if the column is never returned; a sort leaks its order.
- Scoping rows to a tenant or user is the base query's job (`qb.Where("tenant_id = ?", id)`). The
  filters of the request and the condition of a cursor are ANDed with it and cannot widen it.
  squirrel joins `Where` parts with a bare `AND`, so wrap OR conditions of a squirrel base query in
  `squirrel.Or` or parentheses.
- `withDeleted` is ignored unless `AllowWithDeletedInQuery` is set.

## Resource limits

- `limit` is capped at `MaxLimit` (100).
- `paginationMode=disabled` and `limit=-1` (every row) are ignored unless `AllowDisabledPagination`
  is set; the client gets a regular page. Server code that needs every row sets `Request.Mode`
  itself, see [exports](use-cases/exports-and-counts.md).
- `Request.Mode` is the server's word and is applied as is; a client's wish travels in
  `RequestedMode`, which the config gates. A `Request` that arrives as JSON from a client (a
  gateway, another service passing a client's request on) must have `Mode` cleared before
  `Prepare`, or `"mode": "disabled"` reads every row whatever `AllowDisabledPagination` says.
- A page whose offset overflows a 64-bit integer is a 400. Large offsets are still expensive:
  Postgres reads and drops every skipped row. If deep pages matter, page by cursor, where an index
  scan starts at the cursor, or cap `page` in the handler. See [pages and counts](pagination.md).
- `COUNT(*)` reads every matching row on every request. On big tables, take the planner's estimate
  with `CountEstimate` or count nothing with `CountNone`. `ParallelCount` takes two connections
  per request.
- `search` is capped at `MaxSearchLength` characters (256), filter values per request at
  `MaxFilterConditions` (100), list values at `MaxFilterValues` (1000), cursors at 4096 bytes.
- A `filter=` expression is parsed recursively; it is capped at `MaxFilterExpressionComplexity`
  nodes (100), counted while parsing, so deep nesting is refused before it can exhaust the stack.
  Its terms are always checked against the fields, `Lenient` or not.
- `%`, `_` and `\` in `search`, `$ilike` and `$sw` are literal, so a client cannot send expensive
  wildcard patterns. Substring search (`ILIKE '%term%'`) still scans the table: keep searchable
  fields few, or back them with a trigram index (`CREATE INDEX ... USING gin (name gin_trgm_ops)`).
- `$eq`, `$in` and range operators compare with the column's type and can use indexes. With
  `CompareAsText`, `$eq` and `$in` cast the column to text and cannot.

## Links

Links in the answer are built from the request path and the config, never from the Host header:
a client controls that header, and a link with its host could reach other users through a shared
cache. Set `LinksBaseURL` for absolute links. Values in links, cursors included, are URL-encoded.

Relative links start with the path as the server received it, with one exception: a path that
starts with `//` would make protocol-relative links that lead to another host
(`//other.example/users?page=2...`), so its leading slashes collapse into one
(`/other.example/users?page=2...`). A `LinksBaseURL` such as `https://api.example.com` makes the
links absolute.

## Errors

- `*paginate.Error` messages are written for the client. They name the API field and the
  operator, never a column; for a value the database rejected they carry the Postgres message
  (for example `invalid input syntax for type uuid: "x"`). `HTTPStatus` answers them with 400.
- `ErrInvalidConfig` and wrapped database errors are for logs: they may hold SQL, column names and
  values. Answer them with a generic 500.

See [errors](errors.md).

## Checklist for an endpoint

- The base query scopes rows to the caller.
- Only displayable fields are sortable, searchable or filterable.
- Filterable fields have a `Type`; fields with a fixed set of values have an `Enum`.
- `TieBreak` is set, or the tags or the builder set it.
- `Lenient`, `AllowDisabledPagination` and `AllowWithDeletedInQuery` are set only where clients
  need them.
- Big tables page by cursor or count cheaply.
- The endpoint is compiled with `MustCompile` at startup, or a unit test calls `Compile`.
