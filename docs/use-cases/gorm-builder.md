# GORM: tags and the builder

`gormx` builds the fields of a `Config` from a GORM model, in two ways:

- `gormx.ConfigOf[T](db, table)` reads `paginate` tags on the model;
- `gormx.NewBuilder(db, table, &model, &dto)` takes pointers to fields of the model and of a DTO.

Both take the columns from the GORM schema (the naming strategy, `column:` tags, embedded structs
and their prefixes), the types from the Go types of the model, and the primary key as the
tie-breaker when it is one column. `db` supplies the naming strategy; nothing is queried.

## Tags

```go
type BaseModel struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey" paginate:"filter:$in"`
	CreatedAt time.Time      `json:"createdAt" paginate:"sort;filter:$btw"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `json:"-"`
}

type Template struct {
	BaseModel
	Title      string     `json:"title" paginate:"sort;search;filter:$ilike"`
	Price      int64      `json:"price" paginate:"sort;filter:$btw"`
	Moderation Moderation `json:"moderation" gorm:"embedded;embeddedPrefix:moderation_"`
	AuthorID   uuid.UUID  `json:"authorId"`
	Author     User       `json:"author"` // a relation: not a column of templates
}

type Moderation struct {
	Status string `json:"status" paginate:"filter:$in;enum:pending,approved,rejected"`
	Note   string `json:"note"`
}

func templatesConfig(db *gorm.DB) (paginate.Config, error) {
	cfg, err := gormx.ConfigOf[Template](db, "templates")
	if err != nil {
		return paginate.Config{}, err
	}
	cfg.DefaultSort = []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}}
	return cfg, nil
}
```

The fields it makes:

| Name | Column | Type | Clients may |
|---|---|---|---|
| `id` | `templates.id` | `UUID` | filter with `$eq`, `$in` |
| `createdAt` | `templates.created_at` | `Time` | sort; filter with `$eq`, `$btw` |
| `title` | `templates.title` | `Text` | sort, search; filter with `$eq`, `$ilike` |
| `price` | `templates.price` | `Int` | sort; filter with `$eq`, `$btw` |
| `moderation.status` | `templates.moderation_status` | `Enum("pending", "approved", "rejected")` | filter with `$eq`, `$in` |

The tie-breaker is `templates.id`, the primary key; a field tagged `unique` would take its place.
Only tagged fields are API fields: `updatedAt` and `moderation.note` are not. Every setting is in
[configuration](../configuration.md#fields-from-tags).

`paginate.ConfigOf` reads the same tags without GORM: its columns are `table.column`, with the
`db` tag, the gorm `column` tag or the snake_case of the Go name, and it asks for a `column`
setting on fields of nested structs such as `Moderation.Status`. `gormx.ConfigOf` knows the naming
strategy and `gorm:"embedded"`.

## Pointers

The builder reads only the gorm tags of the model, so the same `Template` works with or without
`paginate` tags. The API names come from the DTO the endpoint answers with:

```go
type TemplateDTO struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Price     int64     `json:"price"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Author    AuthorDTO `json:"author"`
}

func templatesConfig(db *gorm.DB) (paginate.Config, error) {
	var m Template
	var d TemplateDTO
	b := gormx.NewBuilder(db, "templates", &m, &d)
	b.Field(&d.ID, &m.ID).Filter(paginate.OpIn)
	b.Field(&d.Title, &m.Title).Sortable().Searchable().Filter(paginate.OpILike)
	b.Field(&d.Price, &m.Price).Sortable().Filter(paginate.OpBtw)
	statuses := paginate.Enum("pending", "approved", "rejected")
	b.Field(&d.Status, &m.Moderation.Status).Type(statuses).Filter(paginate.OpIn) // templates.moderation_status
	b.Field(&d.CreatedAt, &m.CreatedAt).Sortable().Filter(paginate.OpBtw)
	b.DefaultSort(&d.CreatedAt, paginate.Desc)
	return b.Config()
}
```

The same fields, named by the DTO: `status` instead of `moderation.status`. `Filter()` without
operators allows `$eq` only, `Filter(paginate.AllOperators...)` every operator that fits the type.
`Type` replaces the type the Go type gives. `b.TieBreak(&m.Slug)` picks another unique column,
`b.WithoutTieBreak()` turns the tie-breaker off.

## Which one

- **Tags** when the model is what the endpoint answers with, or close to it. The API of the list
  sits next to the columns, one tag per field; `name:` renames a field, `column:` points one at an
  expression.
- **The builder** when the endpoint answers with a DTO whose names or shape differ from the model
  (`status` above, `author.name` below); when the model should not carry API tags (it lives in
  another package, it is generated, several services share it); or when one model has several
  endpoints with different rules: each builder is separate, while tags fix one set per struct. The
  compiler checks the pointers, so a field renamed or removed in the model or the DTO breaks the
  build, not a request.

Either way the result is a plain `paginate.Config`. Set on it what the struct cannot say
(`DefaultSort`, `Pagination`, `Count`, `Format`, limits, more fields), then compile it.

## What both resolve

- Fields of embedded structs, anonymous (`BaseModel`) or tagged `gorm:"embedded"`, with their
  prefix. With the builder, `&m.ID` and `&m.BaseModel.ID` are both `templates.id`.
- Types: `uuid.UUID` is `UUID`, `time.Time` is `Time`, `int64` is `Int`; pointers and `sql.Null*`
  types give the type they hold. See [types](../configuration.md#types).
- API names the way `encoding/json` builds them: the tag, or the Go name without one; fields of
  nested structs as dotted paths (`moderation.status`, `&d.Author.Name` is `author.name`);
  embedded structs promoted.

## What they reject

The builder skips the field and reports it in the error of `Config()`:

- relations (`&m.Author`) and fields inside them (`&m.Author.Name`): they are other tables. Join the
  table in the base query and add the field by hand (below);
- fields GORM ignores (`gorm:"-"`) and fields of unexported embedded structs;
- pointers that are not fields of the model or DTO given to the builder;
- DTO fields tagged `json:"-"`.

```text
builder: model *models.Template: field Author is not a column
builder: model *models.Template: field Author.Name is inside Author, which is neither embedded nor tagged gorm:"embedded"
```

`gormx.ConfigOf` fails as a whole and names every bad tag: unknown settings and types, tags on
fields GORM ignores, tagged fields inside relations without a `column` setting. `Compile` checks
the operators. A relation whose type has `paginate` tags of its own (a `User` with its own
endpoint) brings them along as `author.*` fields, which fail without `column`: tag the relation
`paginate:"-"`.

## Checking at startup

`Config()` returns a usable config even with an error: the fields that resolved. `ConfigOf` returns
none. Either way, check the error once, where the endpoint is built, not per request:

```go
func NewTemplates(db *gorm.DB) (*Templates, error) {
	cfg, err := templatesConfig(db)
	if err != nil {
		return nil, err
	}
	endpoint, err := paginate.Compile(cfg)
	if err != nil {
		return nil, err
	}
	return &Templates{db: db, endpoint: endpoint}, nil
}
```

A test needs no database, only the naming strategy the service uses:

```go
func TestTemplatesConfig(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
	cfg, err := templatesConfig(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := paginate.Compile(cfg); err != nil {
		t.Fatal(err)
	}
}
```

## Table aliases

Both qualify columns with the table they get: with `Table("templates AS t")` pass `"t"`, so the
columns and the tie-breaker are `t.title` and `t.id`. With an empty table, `gormx.ConfigOf` takes
the model's own.

## Fields the model does not have

Add what tags and pointers cannot express to the config afterwards, for example a column of a
joined table:

```go
authorName := b.JSONName(&d.Author.Name) // "author.name"; call it before Config, which reports its errors
cfg, err := b.Config()
if err != nil {
	return paginate.Config{}, err
}
cfg.Fields = append(cfg.Fields, paginate.Field{Name: authorName, Column: "authors.name", Type: paginate.Text, Sortable: true})
```

with `Joins("JOIN authors ON authors.id = templates.author_id")` in the base query. A config from
tags takes such fields the same way. See [joins](joins-and-aliases.md).
