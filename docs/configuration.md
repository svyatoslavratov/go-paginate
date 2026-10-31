# Configuration

A `Config` is the server side of one list endpoint: its fields, how it pages and counts, and its
limits. `Compile` checks it and turns it into an `Endpoint`, once, at startup:

```go
var usersEndpoint = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "id", Column: "users.id", Type: paginate.Int},
		{Name: "createdAt", Column: "users.created_at", Type: paginate.Time, Sortable: true,
			Filter: []paginate.Operator{paginate.OpGte, paginate.OpLt, paginate.OpBtw}},
		{Name: "name", Column: "users.name", Sortable: true, Searchable: true,
			Filter: []paginate.Operator{paginate.OpILike}},
		{Name: "status", Column: "users.status", Type: paginate.Enum("active", "invited", "blocked"),
			Filter: []paginate.Operator{paginate.OpIn}},
	},
	DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
	TieBreak:    "users.id",
})
```

`MustCompile` panics on a broken config, so a mistake stops the service at startup instead of
failing requests one at a time. `Compile` returns the error instead, with every problem listed:
fields without a name or a column, names used twice, unknown operators, operators that do not fit
the field's type, a default sort on unknown fields, cursor pagination without a tie-breaker,
unknown modes and formats, a default limit over the maximum. Its errors wrap `ErrInvalidConfig`.

An endpoint is read-only and safe for any number of concurrent requests. A config that depends
on the request (say, more fields for admins) is two endpoints, compiled once each.

## Fields

A `Field` is everything about one API field in one place:

| Field | Meaning |
|---|---|
| `Name` | The name clients use in `sortBy`, `searchBy`, `filter.<name>` and `filter=`: `createdAt`, `author.name`. |
| `Column` | The SQL it reads: a column as the query names it (`users.created_at`, `u.name` for an alias) or an expression (`COALESCE(u.nickname, u.name)`). Columns come only from the config. |
| `Type` | Checks filter values and cursors before the query runs. See [types](#types). |
| `Sortable` | Clients may sort by it. |
| `Searchable` | `search` matches it. |
| `Filter` | The operators clients may filter it with. `nil`: not filterable. `$eq` is allowed on every filterable field, so `[]Operator{OpEq}` is `$eq` only; `AllOperators` is every operator that fits the type. `$not` goes with any allowed operator. |
| `NotNull` | The column has no NULL values. Cursor pagination then needs no NULL checks for it, so Postgres can start an index scan at the cursor ([speed](pagination.md#speed)). Set it only when it is true: a NULL value would be skipped. |

A field that allows nothing is still useful: the default sort may use it, and cursors read it.

Two fields may read one column under different names, for example during a rename.

## Types

A `Type` checks a value before the query runs, so a value that does not fit is a 400 that names
the field (`invalid value "abc" for price: must be a number`) instead of a database error:

| Type | Accepts |
|---|---|
| `Text` | anything |
| `Int` | a whole number in base 10: `42`, `-7` |
| `Number` | a decimal number: `42`, `-7.5`, `1e3`; not `NaN` or `Inf` |
| `Bool` | `true`, `false`, `1`, `0`, `t`, `f` |
| `Time` | RFC 3339 (`2024-05-01T10:00:00Z`, with a fraction or an offset), the same without an offset, or a date |
| `Date` | `2024-05-01` |
| `UUID` | the canonical form, `8402e99c-5b52-5201-6079-4615a8161683` |
| `Enum("a", "b")` | one of the values, exactly: an allowlist of what clients may filter by |
| `ArrayOf(t)` | an array column; `$contains` checks each of its values against `t` |

The zero `Type` checks nothing. The database then checks the value, and a value it rejects with a
data exception (SQLSTATE class 22) becomes a 400 too, after the round trip.

Patterns of `$ilike` and `$sw` are text whatever the type. `$contains` needs an `ArrayOf` type or
no type at all.

`TypeOf` gives the type of a Go type: `Int` for integers, `Number` for floats, `Time` for
`time.Time`, `UUID` for 16-byte arrays, `ArrayOf` for slices; pointers and `sql.Null*` types give
the type they hold. The tags and the GORM builder use it.

## Sorting

- `DefaultSort`: the sort of a request without `sortBy`, in API names; the first key is the main
  one. Any field may be a key here, sortable or not. Without it the first sortable field is
  sorted ascending.
- `TieBreak`: a column with unique values, usually the primary key. It ends every `ORDER BY`, so
  rows with equal values of the other keys keep their order from page to page. Cursor pagination
  requires it.
- `NullSort`: `NullsFirst` or `NullsLast` for every key; zero keeps the database default (`NULLS
  LAST` for `ASC`, `NULLS FIRST` for `DESC` in Postgres).

See [sorting](sorting.md).

## Pages and counts

- `Pagination`: `OffsetPagination` (page numbers, the default) or `CursorPagination` (keyset).
- `Count`: `CountExact` (the default), `CountEstimate` or `CountNone`.
- `ExactCountBelow`: under `CountEstimate`, an estimate below it is replaced by an exact count.
- `ParallelCount`: run the count and the page at the same time, on two connections.
- `AllowDisabledPagination`: clients may ask for every row with `limit=-1`.

See [pagination](pagination.md) for what each costs and when to choose it.

## Search and filters

- `MultiWordSearch`: every word of the search term must match some searchable field.
- `IgnoreSearchByInQueryParam`: ignore `searchBy`; the search covers every searchable field.
- `CompareAsText`: compare `$eq` and `$in` as text, the behavior before typed comparison.
- `AllowWithDeletedInQuery`: clients may ask for soft-deleted rows with `withDeleted=true`.
- `Lenient`: drop unknown fields and operators that are not allowed instead of answering 400, as
  nestjs-paginate does. Off (the zero value), a typo in a client is a 400 at once. Malformed
  values, values over the caps, filter expressions and cursors fail either way.

See [filters](filters.md) and [query syntax](query-syntax.md).

## Limits and caps

| Field | Default | Caps |
|---|---|---|
| `DefaultLimit` | 20 | the page size of a request without `limit` |
| `MaxLimit` | 100 | the page size a client may ask for |
| `MaxSearchLength` | 256 | the search term, in characters |
| `MaxFilterConditions` | 100 | the `filter.<field>` values in one request, unknown fields included |
| `MaxFilterValues` | 1000 | the values of one `$in`, `$contains` or `$btw` list |
| `MaxFilterExpressionComplexity` | 100 | the nodes of a `filter=` expression |

Zero takes the default. A `DefaultLimit` over `MaxLimit` is cut to it, unless both are set, which
`Compile` reports.

## Answers

- `Format`: `FormatDefault` (this package's shape) or `FormatNestJS` (nestjs-paginate's meta and
  `links`).
- `LinksBaseURL`: the prefix of the links: empty for relative links, `https://api.example.com`
  for absolute ones, `/api` to restore a prefix a proxy strips. The Host header of the request is
  never used.

## Fields from tags

`ConfigOf` builds the fields from `paginate` tags on a struct, written the way gorm tags are:
settings separated by semicolons, values after a colon, lists with commas.

```go
type Product struct {
	ID         int64     `json:"id" paginate:"unique"`
	Name       string    `json:"name" paginate:"sort;search;filter:$ilike,$sw"`
	Price      float64   `json:"price" paginate:"sort;filter:$gte,$lte,$btw"`
	Status     string    `json:"status" paginate:"filter:$in;enum:active,blocked"`
	Tags       []string  `json:"tags" paginate:"filter:$contains;enum:new,sale"`
	ReleasedAt time.Time `json:"releasedAt" paginate:"sort;filter:$btw"`
	Notes      string    `json:"notes"`
}

cfg, err := paginate.ConfigOf[Product]("products")
if err != nil {
	return err
}
cfg.DefaultSort = []paginate.Sort{{Field: "releasedAt", Order: paginate.Desc}}
products := paginate.MustCompile(cfg)
```

| Setting | Meaning |
|---|---|
| `sort` | `Sortable` |
| `search` | `Searchable` |
| `filter` | filterable with `$eq`; `filter:$in,$gt` with these too; `filter:*` with every operator |
| `type:int` | the `Type`: `text`, `int`, `number`, `bool`, `time`, `date`, `uuid`, and `[]int` and the like for arrays |
| `enum:a,b,c` | an `Enum` type; on a slice, an array of the enum |
| `column:expr` | the column, taken as is |
| `name:apiName` | the API name |
| `notnull` | `NotNull` |
| `unique` | the column is the `TieBreak` |

Only fields with a `paginate` tag become API fields: `Notes` above is not one. `paginate:""` adds
a field that allows nothing, `paginate:"-"` skips one.

Without `name`, the API name is the JSON name. Without `type` or `enum`, the Go type decides
(`TypeOf`). Without `column`, the column is `table.column`, where `column` is the `db` tag, the
gorm `column` tag, or the field name in snake_case. Fields of embedded structs are promoted, as
`encoding/json` promotes them; fields of nested structs get dotted names (`author.name`) and need
`column`.

`gormx.ConfigOf[T](db, table)` reads the same tags from a GORM model, with the columns from its
schema (naming strategy, `embedded` structs with a prefix), takes the primary key as the
tie-breaker when no field is tagged `unique`, and sets `NotNull` on the columns GORM knows as `not
null` and on primary keys.

Tags and code describe one endpoint together. `ConfigOf` returns a plain `Config`: code changes the
fields tags cannot describe and adds others, and `Compile` checks the result as a whole.

```go
cfg, err := paginate.ConfigOf[Product]("products")
if err != nil {
	return err
}
cfg.Field("status").Type = paginate.Enum(catalog.Statuses...) // values from Go constants
cfg.Fields = append(cfg.Fields, paginate.Field{               // a joined column
	Name: "brand", Column: "brands.name", Sortable: true, Searchable: true,
})
cfg.DefaultSort = []paginate.Sort{{Field: "releasedAt", Order: paginate.Desc}}
products := paginate.MustCompile(cfg)
```

`cfg.Field` returns nil for a name the config does not have, so a misspelled name panics at
startup. A field appended under a name the tags already gave is a `Compile` error, not an
override: change the existing field through `cfg.Field`.

## Fields from pointers

`gormx.NewBuilder` builds fields from pointers to the fields of a GORM model and an API type:
the column comes from the model, the API name from the DTO's JSON name, the type from the model
field. It suits an API type that differs from the model and code that must not carry API tags.

```go
var model Template
var dto TemplateDTO
b := gormx.NewBuilder(db, "templates", &model, &dto)
b.Field(&dto.ID, &model.ID).Type(paginate.UUID).Filter()
b.Field(&dto.Title, &model.Title).Sortable().Searchable().Filter(paginate.OpILike)
b.Field(&dto.CreatedAt, &model.CreatedAt).Sortable()
b.DefaultSort(&dto.CreatedAt, paginate.Desc)
cfg, err := b.Config()
```

The tie-breaker is the model's primary key when it is one column; `b.TieBreak` and
`b.WithoutTieBreak` change it. A pointer the builder cannot resolve is left out and reported by
`Config`. See [the GORM builder](use-cases/gorm-builder.md).

## Tags, pointers or code

| The fields from | Suit |
|---|---|
| tags, `ConfigOf` | a type that is both the row and the API: the list answers with the model or the scanned row as is |
| pointers, `gormx.NewBuilder` | a model that differs from the API type or must not carry API tags; a renamed field fails to compile |
| code, `Config.Fields` | joins, aliases and SQL expressions, queries without a struct, fields that differ per endpoint |

They mix: tags or pointers give the base and code adds the rest, as in
[Fields from tags](#fields-from-tags). The settings of the endpoint rather than of its fields
(`DefaultSort`, `Pagination`, `Count`, `Format`, the limits) are always code: one struct may serve
several endpoints.

## Checking a config in tests

`Compile` is the check. A test that compiles every endpoint catches a field that lost its column
or an operator that no longer fits:

```go
func TestEndpoints(t *testing.T) {
	if _, err := paginate.Compile(usersConfig()); err != nil {
		t.Fatal(err)
	}
}
```

Endpoints declared with `MustCompile` as package variables are checked by any test of the package.
See [testing](use-cases/testing.md).
