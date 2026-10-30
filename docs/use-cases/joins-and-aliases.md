# Joins, aliases, GROUP BY, subqueries

The base query may be anything the executor can count and page: joins, aliases, groups, subqueries.
The config refers to its columns by the names the query uses.

## Joined columns

Map API fields to columns of joined tables, qualified the way the query names them:

```go
base := db.Table("subscriptions AS s").
	Select("s.*, p.title AS plan_title").
	Joins("JOIN plans AS p ON p.id = s.plan_id")

cfg := paginate.NewConfig("s", []string{"s.created_at", "p.title"}, map[string]string{
	"createdAt": "s.created_at",
	"plan":      "p.title",
	"status":    "s.status",
})
cfg.FilterableColumns = paginate.FilterableColumns{"s.status": {paginate.OpIn}, "p.title": {}}
cfg.TieBreakColumn = "s.id"
```

Always qualify columns in joined queries: a bare `id` is ambiguous as soon as two tables have one.

## One-to-many joins

A join to a to-many relation repeats the parent row once per child, so counts and pages are of
children, not parents. Filter by the relation with `EXISTS` in the base query instead:

```go
base := db.Model(&Template{}).Where(
	"EXISTS (SELECT 1 FROM template_tags tt WHERE tt.template_id = templates.id AND tt.tag = ?)", tag)
```

or group by the parent (next section).

## GROUP BY

```go
base := db.Table("templates").
	Select("templates.id, templates.title, COUNT(l.id) AS likes").
	Joins("LEFT JOIN likes AS l ON l.template_id = templates.id").
	Group("templates.id")
```

- Count: GORM counts the groups; squirrel counts rows of the query as a subquery, also the groups.
- Sorting: grouped columns, columns of a table grouped by its primary key (`templates.title` here),
  aggregates and output aliases. Sorting by a column of the joined table here is a Postgres error
  (42803); do not list such columns as sortable.
- The tie-breaker is added only when the query groups by it (`templates.id` here).
- Filters go into `WHERE`, before grouping: a filter on an aggregate (`likes`) needs a subquery.

## Aggregates and expressions as columns

A column may be any SQL expression the query can evaluate:

```go
cfg.FieldColumns["displayName"] = "COALESCE(p.nickname, u.name)"
```

For sorting by an aggregate, alias it in the select and sort by the alias (`likes`). Filtering by
an aggregate needs the query wrapped in a subquery.

## Subqueries as tables

Wrap the inner query, then filter and sort its output columns:

```go
inner := db.Table("ledger_entries").
	Select("account_id, SUM(amount) AS balance, MAX(created_at) AS last_at").
	Group("account_id")
base := db.Table("(?) AS report", inner)

cfg := paginate.NewConfig("report", []string{"report.balance", "report.last_at"}, map[string]string{
	"balance": "report.balance",
	"lastAt":  "report.last_at",
})
cfg.FilterableColumns = paginate.FilterableColumns{"report.balance": {paginate.OpGte}}
cfg.TieBreakColumn = "report.account_id"
```

Now `filter.balance=$gte:1000` filters the aggregate.

## DISTINCT

`gormx.Paginate` adds no tie-breaker to `DISTINCT` queries (`Distinct()` or a raw
`Select("DISTINCT ...")`): Postgres requires `ORDER BY` columns to be in the select list. Sort by
selected columns only. With squirrel, leave `TieBreakColumn` empty.
