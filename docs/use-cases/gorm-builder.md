# GORM builder

`gormx.NewConfigBuilder` builds a `Config` from pointers to model and DTO fields.
Column names come from the GORM schema (naming strategy, `column:` tags, embedded prefixes), API
names from the DTO's `json` tags. A renamed field fails to compile.

```go
type BaseModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

type Template struct {
	BaseModel
	Title    string
	Price    int64
	Moderation Moderation `gorm:"embedded;embeddedPrefix:moderation_"`
	AuthorID uuid.UUID
	Author   User // a relation: not a column of templates
}

type Moderation struct {
	Status string
	Note   string
}

type TemplateDTO struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Price     int64     `json:"price"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Author    AuthorDTO `json:"author"`
}

func templatesPage(db *gorm.DB) (paginate.Config, error) {
	var m Template
	var d TemplateDTO
	b := gormx.NewConfigBuilder(db, "templates", &m, &d).
		AddSortable(&m.CreatedAt, &m.Title, &m.Price).
		AddSearchable(&m.Title).
		AddFilterable(&m.ID, paginate.OpIn).
		AddFilterable(&m.Price, paginate.OpBtw).
		AddFilterable(&m.Moderation.Status). // $eq only
		MapField(&d.ID, &m.ID).
		MapField(&d.Title, &m.Title).
		MapField(&d.Price, &m.Price).
		MapField(&d.Status, &m.Moderation.Status). // "status" -> templates.moderation_status
		MapField(&d.CreatedAt, &m.CreatedAt).
		AddDefaultSort(&m.CreatedAt, paginate.Desc)
	return b.Build()
}
```

What the builder resolves:

- Fields of embedded structs, anonymous (`BaseModel`) or tagged `gorm:"embedded"`, with their
  prefix. `&m.ID` and `&m.BaseModel.ID` are both `templates.id`. Versions before this fix
  resolved the first field of an embedded struct to the struct itself (`templates.base_model`),
  because both have the same address. Workarounds that map `id` by hand can go.
- `TieBreakColumn`: the primary key of the model when it is a single column. `WithTieBreak(&m.Slug)`
  picks another unique column, `WithoutTieBreak()` turns it off.
- JSON names the way `encoding/json` builds them: the tag, or the field name without one; fields of
  nested DTO structs as dotted paths (`&d.Author.Name` is `author.name`); embedded DTO structs
  promoted.
- `AddFilterable(&m.Field)` without operators allows `$eq` only.

What it rejects, skipping the field and reporting it in the error of `Build()`:

- relations (`&m.Author`) and fields inside them (`&m.Author.Name`): they are other tables. Join the
  table in the base query and map the field to the joined column by hand, see
  [joins](joins-and-aliases.md);
- fields GORM ignores (`gorm:"-"`) and unexported embedded structs;
- pointers that are not fields of the model or DTO given to the builder;
- DTO fields tagged `json:"-"`.

`Build()` returns a usable config even with errors: the unresolved fields are left out. Check the
error once, at startup or in a test, rather than per request:

```go
func TestTemplatesPage(t *testing.T) {
	cfg, err := templatesPage(namingOnlyDB())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

// namingOnlyDB is enough for the builder: it only needs the naming strategy.
func namingOnlyDB() *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
}
```

## Table aliases

The builder qualifies columns with the name it gets: with `Table("templates AS t")` pass `"t"`, so
columns and the tie-breaker are `t.title` and `t.id`.

## Mixing with hand-written entries

`Build()` returns a plain config. Add what the builder cannot express afterwards:

```go
cfg, err := b.Build()
if err != nil {
	return paginate.Config{}, err
}
cfg.FieldColumns["author.name"] = "authors.name"
cfg.SortableColumns = append(cfg.SortableColumns, "authors.name")
```
