# Joins, aliases, GROUP BY, subqueries

The base query may be anything the executor can count and page: joins, aliases, groups, subqueries.
The fields refer to its columns by the names the query uses.

## Joined columns

Point fields at columns of joined tables, qualified the way the query names them:

```go
var subscriptionsEndpoint = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "createdAt", Column: "s.created_at", Type: paginate.Time, Sortable: true},
		{Name: "plan", Column: "p.title", Sortable: true, Filter: []paginate.Operator{paginate.OpEq}},
		{Name: "status", Column: "s.status", Filter: []paginate.Operator{paginate.OpIn}},
	},
	DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
	TieBreak:    "s.id",
})

func (r *Subscriptions) Page(ctx context.Context, req paginate.Request) (*paginate.Response[Subscription], error) {
	base := r.db.Table("subscriptions AS s").
		Select("s.*, p.title AS plan_title").
		Joins("JOIN plans AS p ON p.id = s.plan_id")
	return gormx.Paginate[Subscription](ctx, req, subscriptionsEndpoint, base)
}
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

```go
var templatesEndpoint = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "title", Column: "templates.title", Sortable: true, Searchable: true},
		{Name: "likes", Column: "likes", Type: paginate.Int, Sortable: true}, // the output alias
	},
	DefaultSort: []paginate.Sort{{Field: "likes", Order: paginate.Desc}},
	TieBreak:    "templates.id",
})
```

- Count: GORM counts the groups; squirrel counts the rows of the query as a subquery, also the
  groups.
- Sorting: grouped columns, columns of a table grouped by its primary key (`templates.title` here),
  aggregates and output aliases (`likes`). Sorting by a column of the joined table here is a
  Postgres error (42803) and a 500; do not make such columns sortable.
- The tie-breaker is added only when the query groups by it (`templates.id` here). `gormx` sees a
  column passed to `Group` on its own: `Group("templates.id").Group("templates.title")`, not
  `Group("templates.id, templates.title")`. squirrel appends the tie-breaker to every query: keep
  it only when the query groups by it.
- Filters go into `WHERE`, before grouping: a filter on an aggregate (`likes`) needs a subquery.
- Cursor pagination sorts by columns (`title`), not by aggregates or aliases: its condition goes
  into `WHERE` too, and the second page fails with `column "likes" does not exist`. Wrap the query
  in a subquery for that.

## Expressions as columns

A column may be any SQL expression the query can evaluate:

```go
cfg.Fields = append(cfg.Fields, paginate.Field{
	Name: "displayName", Column: "COALESCE(pr.nickname, people.name)", Sortable: true, Searchable: true,
})
```

For sorting by an aggregate, alias it in the select and sort by the alias (`likes` above).
Filtering by an aggregate needs the query wrapped in a subquery.

In cursor pagination the rows must carry the value: select it too
(`COALESCE(pr.nickname, people.name) AS display_name`) into a field whose JSON name is the API
name (`` DisplayName string `json:"displayName"` ``). An expression has no column name to find it
by.

## Subqueries as tables

Wrap the inner query, then filter and sort its output columns:

```go
inner := db.Table("ledger_entries").
	Select("account_id, SUM(amount) AS balance, MAX(created_at) AS last_at").
	Group("account_id")
base := db.Table("(?) AS report", inner)
```

```go
var balancesEndpoint = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "balance", Column: "report.balance", Type: paginate.Number, Sortable: true,
			Filter: []paginate.Operator{paginate.OpGte}},
		{Name: "lastAt", Column: "report.last_at", Type: paginate.Time, Sortable: true},
	},
	TieBreak: "report.account_id",
})
```

Now `filter.balance=$gte:1000` filters the aggregate, and cursors work on it:

```sql
SELECT * FROM (SELECT account_id, SUM(amount) AS balance, MAX(created_at) AS last_at FROM "ledger_entries" GROUP BY "account_id") AS report WHERE report.balance >= $1 ORDER BY report.balance ASC,report.account_id ASC LIMIT $2
```

## DISTINCT

`gormx.Paginate` adds no tie-breaker to `DISTINCT` queries (`Distinct()` or a raw
`Select("DISTINCT ...")`): Postgres requires `ORDER BY` columns to be in the select list. Sort by
selected columns only. Cursor pagination, which needs the tie-breaker, fails on them. With
squirrel, leave `TieBreak` empty.

GORM counts `Distinct("templates.title")` with `COUNT(DISTINCT(...))`, but counts a `Distinct` of
several columns, and a raw `Select("DISTINCT ...")`, with `count(*)` of every row: the total comes
out too big. Wrap such a query in a subquery, which counts what it returns:

```go
inner := db.Table("templates").Distinct("templates.title", "templates.status")
base := db.Table("(?) AS t", inner)
```

with fields on `t.title` and `t.status`, and a `TieBreak` only if a column of the subquery is
unique.
