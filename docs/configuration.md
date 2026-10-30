# Configuration

`Config` is the server side of one endpoint. Build it once (a package variable or a
constructor), not per request.

```go
cfg := paginate.NewConfig(tableName, sortableColumns, mapInputFieldsToEntityColumns)
```

`NewConfig` sets the default limits and `StrictValidate: true`. A struct literal starts
with `StrictValidate: false`. Zero numeric fields take the `Default*` constants either way.

## Fields

Names and columns:

- `TableName` (required): the table. The builder qualifies columns with it; validation requires it.
- `FieldColumns`: API field → column, for `sortBy`, `searchBy` and `filter`. Every
  field a client may use must be here. Columns are SQL as the query names them: `users.name`,
  `c.code` for an alias, an expression such as `COALESCE(p.name, u.name)`. Columns come only from
  the config, so they may be any SQL the query can evaluate.

Sorting ([details](sorting.md)):

- `SortableColumns` (required): columns clients may sort by. The first one is the sort of last resort.
- `DefaultSortBy`: the sort of a request without `sortBy`.
- `DefaultSortKeys`: the priority of the `DefaultSortBy` keys. The builder fills it.
- `TieBreakColumn`: a unique column appended to every `ORDER BY`.
- `NullSort`: `NULLS FIRST` or `NULLS LAST` for every key.

Search:

- `SearchableColumns`: columns matched by `search`.
- `MultiWordSearch`: every word of the term must match.
- `IgnoreSearchByInQueryParam`: ignore `searchBy`.
- `MaxSearchLength` (256): the longest term, in characters.

Filters ([details](filters.md)):

- `FilterableColumns`: column → allowed operators. `{}` means `$eq` only, `AllOperators`
  means every operator.
- `CompareAsText`: compare `$eq` and `$in` as text, the behavior before typed comparison.
- `MaxFilterConditions` (100): filter values in one request.
- `MaxFilterValues` (1000): values in one `$in`, `$contains` or `$btw` list.
- `MaxFilterExpressionComplexity` (100): nodes of a `filter=` expression.

The answer:

- `Format`: `FormatDefault` (this package's meta) or `FormatNestJS`
  (nestjs-paginate's meta and `links`). See [the answer in nestjs-paginate's shape](migrating-from-nestjs.md#the-answer-in-nestjs-paginates-shape).
- `LinksBaseURL`: the prefix of the links: empty for relative links, an origin
  (`https://api.example.com`) for absolute ones, a path (`/api`) for a prefix a proxy strips.

Limits and modes:

- `DefaultLimit` (20), `MaxLimit` (100): page size. `limit=0` asks for the count only.
- `AllowDisabledPagination`: clients may ask for every row (`paginationMode=disabled`, `limit=-1`).
- `AllowWithDeletedInQuery`: clients may ask for soft-deleted rows (`withDeleted=true`, GORM only).
- `StrictValidate`: unknown fields and operators outside the lists are a 400. Off, they are dropped.
  Malformed values and values over the caps are a 400 either way.

## Validate

Requests check only what they need, so a broken config fails one request at a time, for example
only the requests that sort by the broken column. `Validate` checks everything at once:

```go
func TestUsersPageConfig(t *testing.T) {
	if err := usersPage.Validate(); err != nil {
		t.Fatal(err)
	}
}
```

It reports:

- a missing table or sortable columns;
- `DefaultLimit` above `MaxLimit`;
- an unknown `Format`;
- sortable, searchable and filterable columns that no API field maps to (clients cannot reach them;
  often the config lists the API name instead of the column);
- unknown operators and orders;
- fields the GORM builder could not resolve.
