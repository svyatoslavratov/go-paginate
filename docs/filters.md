# Filters

A filter is a query parameter `filter.<field>=<value>`, where the value is

```
[$or: | $and:] [$not:] [$operator:] value
```

The prefixes may come in any order, each at most once. Everything after them is the value, so it
may contain `:` (`$eq:12:30` is `12:30`) or operator-like text (`$eq:$gt` is `$gt`).

## Allowing filters

A field is filterable when the config maps it to a column (`FieldColumns`) and
lists the column in `FilterableColumns` with its operators:

```go
cfg.FilterableColumns = paginate.FilterableColumns{
	"users.status": {},                                      // $eq only
	"users.age":    {paginate.OpGte, paginate.OpLt},
	"users.name":   paginate.AllOperators,             // everything
}
```

`$eq` is always allowed, so an empty list allows `$eq` only. `$not` is allowed with any allowed
operator. An unknown field or an operator outside the list is a 400 with `StrictValidate`, and is
dropped without it.

## Operators

| Operator | Example | Condition |
|---|---|---|
| `$eq` | `filter.status=active`, `filter.status=$eq:active` | `status = $1` |
| `$gt` | `filter.age=$gt:18` | `age > $1` |
| `$gte` | `filter.age=$gte:18` | `age >= $1` |
| `$lt` | `filter.age=$lt:65` | `age < $1` |
| `$lte` | `filter.age=$lte:65` | `age <= $1` |
| `$in` | `filter.id=$in:1,2,3` | `id IN ($1, $2, $3)` |
| `$btw` | `filter.age=$btw:18,65` | `age >= $1 AND age <= $2` (one value: `age >= $1`) |
| `$null` | `filter.seenAt=$null` | `seen_at IS NULL` |
| `$ilike` | `filter.title=$ilike:red` | `CAST(title AS TEXT) ILIKE '%red%'` |
| `$sw` | `filter.title=$sw:red` | `CAST(title AS TEXT) ILIKE 'red%'` |
| `$contains` | `filter.roles=$contains:admin,editor` | `roles @> '{"admin","editor"}'` |

`$not` wraps the condition: `filter.id=$not:$in:2,5,7` is `NOT (id IN (2, 5, 7))`,
`filter.seenAt=$not:$null` is `NOT (seen_at IS NULL)`. As everywhere in SQL, a negated comparison
does not match NULL.

## Values and types

Values are bind parameters, never SQL text. They are sent as text and Postgres reads them as the
column's type, so:

- comparisons use indexes;
- `$eq:10.5` matches a numeric `10.50`, `$gt:2024-01-01` compares dates;
- a value that does not fit the type (`abc` for an integer, a malformed uuid or date) is a 400
  (`ErrInvalidRequest`, from the Postgres data exception).

`CompareAsText` restores the old text comparison for `$eq` and `$in`: `CAST(column AS TEXT) = $1`.
A wrong value then just matches nothing, but no index is used and `10.5` does not match `10.50`.

Dates: `$btw:2024-01-01,2024-01-31` on a timestamp ends at midnight of January 31. Use the next
day, or a full timestamp, as the upper bound.

Lists (`$in`, `$contains`, `$btw`) are split on commas and trimmed: `$in:1, 2` is `1` and `2`. A
value cannot contain a comma. A list is capped at `MaxFilterValues` values (1000).

`$ilike` and `$sw` match text: the column is cast to text, so they work on numbers and uuids too.
`%`, `_` and `\` in the value are literal: `$ilike:50%` finds "50%", not "50 and anything".

`$contains` is for array columns: the column must contain every value. The values travel as one
Postgres array literal that takes the column's array type (`text[]`, `int[]`, `uuid[]`). On a
non-array column it is a SQL error, so allow it on array columns only.

## Several conditions

Conditions of one field form a chain in request order. Each one joins the previous with its own
comparator: `$and` (the default) or `$or`. AND binds tighter than OR, as in SQL:

```http
?filter.id=$gt:3&filter.id=$lt:5&filter.id=$or:$eq:7
```

```sql
((id > 3) AND (id < 5) OR (id = 7))
```

```http
?filter.id=5&filter.id=$or:7
```

```sql
((id = 5) OR (id = 7))
```

Different fields are ANDed. Fields whose first condition starts with `$or` form one OR group, ANDed
with the other fields:

```http
?filter.shopId=$or:2&filter.name=$or:$sw:big&filter.status=active
```

```sql
((shop_id = 2) OR (CAST(name AS TEXT) ILIKE 'big%')) AND (status = 'active')
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
((color = 'black') OR (color = 'white')) AND (NOT (name = 'Leche'))
```

- Precedence: `NOT` binds tighter than `AND`, `AND` tighter than `OR`. Keywords are
  case-insensitive.
- Quote a value with whitespace, parentheses or a keyword: `name=$eq:"Cat Mansion"`,
  `name=$eq:'a AND b'`. Inside quotes a backslash escapes a quote or a backslash
  (`"Milo \"the cat\""`); other backslashes stay (`"C:\Users"`). Quoted and unquoted parts of a
  value join: `foo" bar"baz` is `foo barbaz`.
- `field=` with nothing after `=` filters by an empty string.
- Every term is checked like `filter.<field>`, but always strictly: an unknown field or an
  operator outside the list is a 400 even without `StrictValidate`, since dropping a term would
  change what the expression means.
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

Going over a cap is a 400, with or without `StrictValidate`.
