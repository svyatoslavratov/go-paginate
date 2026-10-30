# Errors

Every error of this package is one of two kinds:

- `ErrInvalidRequest`: the client's mistake. Answer 400; the message is safe to show.
- `ErrInvalidConfig`: the server's mistake, a broken `Config`. `Compile` reports it at startup.

Anything else (a database that is down, a timeout) is wrapped as it came. `HTTPStatus` picks the
status:

```go
page, err := gormx.Paginate[User](r.Context(), *req, usersEndpoint, db.Model(&User{}))
if err != nil {
	status := paginate.HTTPStatus(err) // 400 for the client's mistakes, 500 for the rest
	if status == http.StatusBadRequest {
		writeJSON(w, status, err) // {"code": ..., "field": ..., "message": ...}
		return
	}
	log.Printf("list users: %v", err)
	writeJSON(w, status, map[string]string{"message": "internal error"})
	return
}
```

## The error

Client mistakes are a `*paginate.Error`:

```go
type Error struct {
	Code    Code   // what is wrong, stable for clients
	Field   string // the API field it is about, empty for the request as a whole
	Message string // safe to show; it names API fields, never columns
	Err     error  // ErrInvalidRequest or ErrInvalidConfig
}
```

Its JSON is the body of a 400:

```json
{"code": "invalid_value", "field": "price", "message": "invalid value \"abc\" for price: must be a number"}
```

A client can mark the filter `field` names and show a message in its own language for `code`.
In Go, `errors.As` gives the same:

```go
var pe *paginate.Error
if errors.As(err, &pe) && pe.Code == paginate.CodeUnknownField {
	// ...
}
```

## Codes

| Code | Field | When |
|---|---|---|
| `invalid_page` | | the page is so large its offset does not fit a 64-bit integer |
| `invalid_sort` | the field, if known | a `sortBy` value is not `field:ASC` or `field:DESC` |
| `sort_not_allowed` | ✅ | the field is unknown or not sortable |
| `search_not_allowed` | ✅ | a `searchBy` field is unknown or not searchable |
| `search_too_long` | | the search term is over `MaxSearchLength` |
| `unknown_field` | ✅ | a filter names a field the endpoint does not have |
| `filter_not_allowed` | ✅ | the field is not filterable |
| `operator_not_allowed` | ✅ | the operator is not allowed on the field |
| `invalid_value` | ✅ (see below) | a value is empty, malformed, or does not fit the field's type |
| `too_many_values` | the field, for a list | more filter values than `MaxFilterConditions`, or a list over `MaxFilterValues` |
| `invalid_expression` | the field, if one is at fault | `filter=` does not parse, is too complex, or uses `$and`/`$or` |
| `invalid_cursor` | | the cursor does not decode, was made for another sort, or holds values that do not fit |
| `invalid_config` | | the config is broken (`ErrInvalidConfig`) |

`Lenient` drops unknown fields and operators that are not allowed instead of failing with
`unknown_field`, `filter_not_allowed`, `sort_not_allowed`, `search_not_allowed` and
`operator_not_allowed`. Every other code fails either way.

## Values the database rejects

A field with a [type](configuration.md#types) checks its values before the query runs. A field
without one leaves it to Postgres: `abc` for an integer column is a data exception, SQLSTATE class
22. When the request carries client values (filters or a cursor), executors turn that into
`invalid_value` too, through `Query.WrapError`, with the database's message and no `field`:

```json
{"code": "invalid_value", "message": "invalid filter value: ERROR: invalid input syntax for type integer: \"abc\" (SQLSTATE 22P02)"}
```

The database error stays reachable with `errors.As` for logs. Give fields a type to get the
field name and a message of your own words instead, and to skip the round trip.

## Configs

`Compile` lists every problem of a config in one `invalid_config` error:

```text
paginate: invalid config: field rating: $contains needs an array type, the field is int
default sort: unknown field createdAt
no sort: set DefaultSort, make a field sortable, or set TieBreak
```

`MustCompile` panics with it, so a broken endpoint declared as a package variable stops the
service at startup.
