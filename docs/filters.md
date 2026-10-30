# Filters

A filter is a query parameter `filter.<field>=<value>`, where the value is

```
[$or: | $and:] [$not:] [$operator:] value
```

The prefixes may come in any order, each at most once. Everything after them is the value, so it
may contain `:` (`$eq:12:30` is `12:30`) or operator-like text (`$eq:$gt` is `$gt`).

## Allowing filters

A field is filterable when its `Filter` lists the operators clients may use:

```go
cfg := paginate.Config{
	Fields: []paginate.Field{
		{Name: "status", Column: "users.status", Filter: []paginate.Operator{paginate.OpEq}}, // $eq only
		{Name: "age", Column: "users.age", Type: paginate.Int,
			Filter: []paginate.Operator{paginate.OpGte, paginate.OpLt}},
		{Name: "name", Column: "users.name", Filter: paginate.AllOperators}, // every operator
		{Name: "email", Column: "users.email", Searchable: true},            // not filterable
	},
	TieBreak: "users.id",
}
```

A `nil` `Filter` is not filterable. `$eq` is always allowed on a filterable field, so
`[]Operator{OpEq}` allows `$eq` only. `AllOperators` is every operator that fits the field's type:
on a field whose type is not an array, it leaves out `$contains`. `$not` is allowed with any
allowed operator. Tags (`filter:$in,$gt`) and the GORM builder (`.Filter(paginate.OpIn)`) set the
same list, see [configuration](configuration.md).

A filter on an unknown field is a 400 with `unknown_field`, on a field that is not filterable
with `filter_not_allowed`, with an operator outside the list with `operator_not_allowed`. With
`Lenient` the field or the value is dropped instead. Values that are malformed or do not fit the
type fail either way.

## Operators

Columns are written as the config names them; `$1`, `$2` are the arguments.

| Operator | Example | Condition | Arguments |
|---|---|---|---|
| `$eq` | `filter.status=active`, `filter.status=$eq:active` | `status = $1` | `active` |
| `$gt` | `filter.age=$gt:18` | `age > $1` | `18` |
| `$gte` | `filter.age=$gte:18` | `age >= $1` | `18` |
| `$lt` | `filter.age=$lt:65` | `age < $1` | `65` |
| `$lte` | `filter.age=$lte:65` | `age <= $1` | `65` |
| `$in` | `filter.id=$in:1,2,3` | `id IN ($1, $2, $3)` | `1`, `2`, `3` |
| `$btw` | `filter.age=$btw:18,65` | `age >= $1 AND age <= $2` (one value: `age >= $1`) | `18`, `65` |
| `$null` | `filter.seenAt=$null` | `seen_at IS NULL` | none |
| `$ilike` | `filter.title=$ilike:red` | `CAST(title AS TEXT) ILIKE $1` | `%red%` |
| `$sw` | `filter.title=$sw:red` | `CAST(title AS TEXT) ILIKE $1` | `red%` |
| `$contains` | `filter.roles=$contains:admin,editor` | `roles @> $1` | `{"admin","editor"}` |

`$not` wraps the condition: `filter.id=$not:$in:2,5,7` is `NOT (id IN ($1, $2, $3))`,
`filter.seenAt=$not:$null` is `NOT (seen_at IS NULL)`. As everywhere in SQL, a negated comparison
does not match NULL.

## Values and types

Values are bind parameters, never SQL text. Before the query runs, `Prepare` checks them against
the field's `Type`. A value that does not fit is a 400 with `invalid_value` that names the field,
and no query runs:

```json
{"code":"invalid_value","field":"age","message":"invalid value \"old\" for age: must be an integer"}
```

What is checked:

- `$eq`, `$gt`, `$gte`, `$lt`, `$lte`: the value.
- `$in` and `$btw`: every value of the list. `$btw` takes one or two values.
- `$contains`: every value, against the element type of an `ArrayOf`.
- `$ilike` and `$sw`: nothing. A pattern is text whatever the type, so `$sw:20` works on an
  integer column.
- `$null`: there is no value.
- Operators other than `$contains` on an `ArrayOf` field: nothing. `$eq` compares whole arrays,
  and the database reads the literal.

`Int`, `Number`, `Bool`, `Time`, `Date` and `UUID` check the format, `Enum("draft", "published")`
is an allowlist compared exactly, `Text` takes anything. See [types](configuration.md#types) for
what each accepts. Tags and the GORM builder take the type from the Go type of the field.

A field without a type leaves the check to the database. Values are sent as text and Postgres
reads them as the column's type; a value that does not fit (`abc` for an integer, a malformed uuid
or date) is a data exception, which the executors turn into a 400 with `invalid_value` too: after
the round trip, and without the field. See [errors](errors.md#values-the-database-rejects).

Either way the comparison uses the column's type, so:

- comparisons use indexes;
- `$eq:10.5` matches a numeric `10.50`, `$gt:2024-01-01` compares dates.

`CompareAsText` restores the old text comparison for `$eq` and `$in`: `CAST(column AS TEXT) = $1`.
No index is used and `10.5` does not match `10.50`. Values are still checked against the type; on
a field without one, a value of the wrong type then just matches nothing.

Dates: `$btw:2024-01-01,2024-01-31` on a timestamp ends at midnight of January 31. Use the next
day, or a full timestamp, as the upper bound.

Lists (`$in`, `$contains`, `$btw`) are split on commas and trimmed: `$in:1, 2` is `1` and `2`. A
value cannot contain a comma. A list is capped at `MaxFilterValues` values (1000).

`$ilike` and `$sw` match text: the column is cast to text, so they work on numbers and uuids too.
`%`, `_` and `\` in the value are literal: `$ilike:50%` finds "50%", not "50 and anything".

`$contains` is for array columns: the column must contain every value. The values travel as one
Postgres array literal that takes the column's array type (`text[]`, `int[]`, `uuid[]`). Give such
a field an `ArrayOf` type: `Compile` refuses `$contains` on a field of any other type, and
`AllOperators` leaves it out there. On a field without a type, `$contains` on a column that is not
an array is a SQL error, so allow it on array columns only.

## Several conditions

Conditions of one field form a chain in request order. Each one joins the previous with its own
comparator: `$and` (the default) or `$or`. AND binds tighter than OR, as in SQL:

```http
?filter.id=$gt:3&filter.id=$lt:5&filter.id=$or:$eq:7
```

```sql
((id > $1) AND (id < $2) OR (id = $3))  -- 3, 5, 7
```

```http
?filter.id=5&filter.id=$or:7
```

```sql
((id = $1) OR (id = $2))  -- 5, 7
```

Different fields are ANDed, in the order of their names. Fields whose first condition starts with
`$or` form one OR group, ANDed with the other fields:

```http
?filter.shopId=$or:2&filter.name=$or:$sw:big&filter.status=active
```

```sql
(((CAST(name AS TEXT) ILIKE $1) OR (shop_id = $2)) AND (status = $3))  -- big%, 2, active
```

These are the multi-filter rules of nestjs-paginate up to version 14. Version 15 dropped them in
favor of filter expressions; both work here.

## Filter expressions

`filter=<expression>` combines terms with `AND`, `OR`, `NOT` and parentheses, the way
nestjs-paginate 15+ does. A term is `field=value`, where the value is a filter value as above
(`$not` and an operator allowed, `$and`/`$or` not: the expression joins the terms).

```http
?filter=color=$eq:black AND age=$gte:3
?filter=(color=$eq:black OR color=$eq:white) AND NOT name=$eq:Leche
?filter=status=$in:paid,shipped OR (total=$gte:1000 AND NOT comment=$null)
```

```sql
(((color = $1) OR (color = $2)) AND (NOT (name = $3)))  -- black, white, Leche
```

- Precedence: `NOT` binds tighter than `AND`, `AND` tighter than `OR`. Keywords are
  case-insensitive.
- Quote a value with whitespace, parentheses or a keyword: `name=$eq:"Cat Mansion"`,
  `name=$eq:'a AND b'`. Inside quotes a backslash escapes a quote or a backslash
  (`"Milo \"the cat\""`); other backslashes stay (`"C:\Users"`). Quoted and unquoted parts of a
  value join: `foo" bar"baz` is `foo barbaz`.
- `field=` with nothing after `=` filters by an empty string, which the field's type must accept.
- Every term is checked like `filter.<field>`, types included, but always strictly: an unknown
  field or an operator outside the list is a 400 even with `Lenient`, since dropping a term would
  change what the expression means.
- An expression that does not parse, a term with `$and` or `$or`, or an expression over the cap
  is a 400 with `invalid_expression`.
- The expression is ANDed with the `filter.<field>` parameters and the search. Repeated `filter=`
  parameters are ANDed with each other.
- Every term, `AND`, `OR`, `NOT` and parenthesized group is a node; an expression over
  `MaxFilterExpressionComplexity` nodes (100) is a 400. The cap also bounds how deep the parser
  recurses.
- URL-encode the value: `?filter=color%3D$eq:black+AND+age%3D$gte:3`. The examples here are
  decoded.

The value-level `$not` (`color=$not:$eq:white`) negates one comparison; the keyword `NOT` negates a
term or a group.

## Caps

- `MaxFilterConditions` (100): filter values in one request, unknown fields included.
- `MaxFilterValues` (1000): values in one list.
- `MaxFilterExpressionComplexity` (100): nodes of a filter expression.

Going over a cap is a 400 (`too_many_values`, or `invalid_expression` for an expression), with or
without `Lenient`.
