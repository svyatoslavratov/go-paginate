# Migrating from nestjs-paginate

Each section shows a feature of [nestjs-paginate 16.0.1](https://github.com/ppetzold/nestjs-paginate)
and the same feature in Go. The frontend keeps sending the same queries. The
[comparison](nestjs-compatibility.md) lists what differs.

The examples use the cats of the nestjs-paginate README.

## Entity and model

```ts
@Entity()
export class CatEntity {
  @PrimaryGeneratedColumn() id: number
  @Column('text') name: string
  @Column('text') color: string
  @Column('int') age: number
  @Column({ nullable: true }) lastVetVisit: Date | null
  @CreateDateColumn() createdAt: string
}
```

```go
type Cat struct {
	ID           int64 `gorm:"primaryKey"`
	Name         string
	Color        string
	Age          int
	LastVetVisit *time.Time
	CreatedAt    time.Time
}

// The API names of the fields, what clients send in sortBy and filter.
type CatDTO struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Color        string     `json:"color"`
	Age          int        `json:"age"`
	LastVetVisit *time.Time `json:"lastVetVisit"`
	CreatedAt    time.Time  `json:"createdAt"`
}
```

nestjs-paginate names columns by entity keys. Here clients name API fields, and the config maps
them to SQL columns (`FieldColumns`), so the API and the schema can differ.

## Endpoint

```ts
@Controller('cats')
export class CatsController {
  constructor(private readonly catsService: CatsService) {}

  @Get()
  findAll(@Paginate() query: PaginateQuery): Promise<Paginated<CatEntity>> {
    return this.catsService.findAll(query)
  }
}
```

```go
func (h *CatsHandler) FindAll(w http.ResponseWriter, r *http.Request) {
	query, err := paginate.ParseRequest(r) // @Paginate()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := h.cats.FindAll(r.Context(), *query)
	switch {
	case errors.Is(err, paginate.ErrInvalidRequest): // BadRequestException
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, page)
	}
}
```

With fasthttp, use `fasthttpx.ParseRequest(ctx)`.

## paginate(query, repo, config)

```ts
public findAll(query: PaginateQuery): Promise<Paginated<CatEntity>> {
  return paginate(query, this.catsRepository, {
    sortableColumns: ['id', 'name', 'color', 'age'],
    nullSort: 'last',
    defaultSortBy: [['id', 'DESC']],
    searchableColumns: ['name', 'color', 'age'],
    filterableColumns: {
      name: [FilterOperator.EQ, FilterSuffix.NOT],
      age: true,
    },
  })
}
```

```go
var catsPage = func() paginate.Config {
	cfg := paginate.NewConfig("cats",
		[]string{"cats.id", "cats.name", "cats.color", "cats.age"}, // sortableColumns
		map[string]string{
			"id":    "cats.id",
			"name":  "cats.name",
			"color": "cats.color",
			"age":   "cats.age",
		})
	cfg.NullSort = paginate.NullsLast // nullSort: 'last'
	cfg.DefaultSortBy = paginate.SortBy{"cats.id": paginate.Desc} // defaultSortBy
	cfg.SearchableColumns = []string{"cats.name", "cats.color", "cats.age"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"cats.name": {},                          // $eq; $not is always allowed
		"cats.age":  paginate.AllOperators, // true
	}
	cfg.TieBreakColumn = "cats.id"
	return *cfg
}()

func (s *CatsService) FindAll(ctx context.Context, query paginate.Request) (*paginate.Response[Cat], error) {
	return gormx.Paginate[Cat](ctx, query, catsPage, s.db.Model(&Cat{}))
}
```

Or let the [GORM builder](use-cases/gorm-builder.md) take the columns from the model:

```go
var m Cat
var d CatDTO
cfg, err := gormx.NewConfigBuilder(db, "cats", &m, &d).
	AddSortable(&m.ID, &m.Name, &m.Color, &m.Age).
	AddSearchable(&m.Name, &m.Color, &m.Age).
	AddFilterable(&m.Name).
	AddFilterable(&m.Age, paginate.AllOperators...).
	MapField(&d.ID, &m.ID).MapField(&d.Name, &m.Name).
	MapField(&d.Color, &m.Color).MapField(&d.Age, &m.Age).
	AddDefaultSort(&m.ID, paginate.Desc).
	Build() // err lists fields the builder could not resolve
```

## The answer in nestjs-paginate's shape

```ts
// Paginated<CatEntity>: { data, meta: { ..., sortBy: [['id', 'DESC']] }, links: { ... } }
```

```go
cfg.Format = paginate.FormatNestJS
cfg.LinksBaseURL = "https://api.example.com" // origin; empty gives relative links (relativePath: true)
```

```json
{
  "data": [{ "id": 4, "name": "George", "color": "white", "age": 3 }],
  "meta": {
    "itemsPerPage": 5,
    "totalItems": 12,
    "currentPage": 2,
    "totalPages": 3,
    "sortBy": [["color", "DESC"]],
    "search": "i",
    "searchBy": ["name", "color", "age"],
    "filter": { "age": "$gte:3" }
  },
  "links": {
    "first": "https://api.example.com/cats?page=1&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3",
    "previous": "https://api.example.com/cats?page=1&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3",
    "current": "https://api.example.com/cats?page=2&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3",
    "next": "https://api.example.com/cats?page=3&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3",
    "last": "https://api.example.com/cats?page=3&limit=5&sortBy=color:DESC&search=i&filter.age=$gte:3"
  }
}
```

nestjs-paginate builds absolute links from the request's host. Here links are relative until you
set `LinksBaseURL`: the Host header comes from the client and must not end up in links others may
follow. Behind a proxy that strips a prefix, put it into `LinksBaseURL` (`"/api"`).

A handler that maps rows to DTOs keeps the format by copying the meta whole:

```go
writeJSON(w, http.StatusOK, paginate.Response[CatDTO]{Data: toDTOs(page.Data), Meta: page.Meta})
```

A handler that answers with its own generated types reads the same data from `page.Meta.NestJS()`
and `page.Meta.Links()`.

## Query builder

```ts
const queryBuilder = repo
  .createQueryBuilder('cats')
  .leftJoinAndSelect('cats.owner', 'owner')
  .where('cats.owner = :ownerId', { ownerId })

const result = await paginate<CatEntity>(query, queryBuilder, config)
```

```go
base := db.Model(&Cat{}).
	Joins("LEFT JOIN owners AS owner ON owner.id = cats.owner_id").
	Where("cats.owner_id = ?", ownerID)

page, err := gormx.Paginate[Cat](ctx, query, catsPage, base)
```

With squirrel and pgx:

```go
qb := squirrel.Select("cats.id", "cats.name", "cats.color", "cats.age").
	From("cats").
	LeftJoin("owners AS owner ON owner.id = cats.owner_id").
	Where(squirrel.Eq{"cats.owner_id": ownerID})

page, err := squirrelx.Paginate(ctx, query, catsPage, pool, scanCats, qb)
```

## where

```ts
paginate(query, repo, { ...config, where: { color: 'black' } })
```

```go
gormx.Paginate[Cat](ctx, query, catsPage, db.Model(&Cat{}).Where("cats.color = ?", "black"))
```

## Relations

```ts
const config: PaginateConfig<CatEntity> = {
  relations: { toys: true },
  sortableColumns: ['id', 'name', 'toys.name'],
  filterableColumns: { 'toys.name': [FilterOperator.IN] },
}
// ?filter.toys.name=$in:Mouse,String
```

A to-one relation is a join and a mapped field:

```go
base := db.Model(&Cat{}).Joins("JOIN homes AS home ON home.id = cats.home_id")
cfg.FieldColumns["home.name"] = "home.name" // ?filter.home.name=$eq:House
cfg.FilterableColumns["home.name"] = []paginate.Operator{}
```

A to-many relation (`toys`) would repeat cats per toy in a join. Filter by it with `EXISTS` in the
base query, from your own parameter:

```go
base := db.Model(&Cat{}).Where(
	"EXISTS (SELECT 1 FROM toys WHERE toys.cat_id = cats.id AND toys.name IN ?)", toyNames)
```

See [joins, aliases, GROUP BY](use-cases/joins-and-aliases.md).

## Filter expressions

Same syntax:

```http
GET /cats?filter=(color=$eq:black OR color=$eq:white) AND NOT name=$eq:Leche
GET /cats?filter=name=$eq:"Milo \"the cat\""
```

```ts
const config = { ..., filterExpressionMaxComplexity: 50 }
```

```go
cfg.MaxFilterExpressionComplexity = 50
```

Terms are validated against `FilterableColumns` like `filter.<field>`, and an unknown field or an
operator outside the list is a 400 whatever `StrictValidate` says.

## $or inside filter.\<field\>

nestjs-paginate 15 removed `$and`/`$or` from `filter.<field>`. They still work here, with the
rules of version 14. Prefer `filter=` in new code:

```http
GET /cats?filter.id=5&filter.id=$or:7      # still works here
GET /cats?filter=id=$eq:5 OR id=$eq:7      # the same, as in nestjs-paginate 15+
```

## Count only and every row

```http
GET /cats?limit=0          # count only: data [], meta.totalItems
GET /cats?limit=-1         # every row, when the server allows it
```

```ts
const config = { ..., maxLimit: -1 } // allow limit=-1
```

```go
cfg.AllowDisabledPagination = true // allow limit=-1 and paginationMode=disabled
```

Server code that needs every row (an export) sets the mode itself and needs no option:

```go
query.Mode = paginate.ModeDisabled
```

## Soft deletes

```ts
const config = { ..., allowWithDeletedInQuery: true } // ?withDeleted=true
```

```go
cfg.AllowWithDeletedInQuery = true // GORM: the request runs Unscoped
```

## Multi-word search and searchBy

```ts
const config = { ..., multiWordSearch: true, ignoreSearchByInQueryParam: true }
```

```go
cfg.MultiWordSearch = true
cfg.IgnoreSearchByInQueryParam = true
```

## Invalid input

```ts
const config = { ..., throwOnInvalidFilter: true } // 400 for invalid filter.<field>
```

```go
cfg.StrictValidate = true // NewConfig sets it; covers sortBy and searchBy too
```

nestjs-paginate drops unknown fields by default. To drop them here too, set
`StrictValidate = false`. Over-the-cap values, malformed lists and filter expressions are a 400
either way.

## Global defaults

```ts
updateGlobalConfig({ defaultLimit: 20, defaultMaxLimit: 100, defaultOrigin: 'https://api.example.com' })
```

```go
// Build every config of a service through one constructor.
func newPageConfig(table string, sortable []string, fields map[string]string) *paginate.Config {
	cfg := paginate.NewConfig(table, sortable, fields)
	cfg.DefaultLimit, cfg.MaxLimit = 20, 100
	cfg.Format = paginate.FormatNestJS
	cfg.LinksBaseURL = "https://api.example.com"
	return cfg
}
```

## Swagger

```ts
@PaginatedSwaggerDocs(UserDto, USER_PAGINATION_CONFIG)
```

Describe the parameters and the answer in the OpenAPI spec, see [OpenAPI](use-cases/openapi.md).

## Not supported

- `select=`: the DTO decides the fields.
- Cursor pagination (`paginationType: CURSOR`).
- `relations` in the config, to-many `EXISTS` filters and the `$any`/`$all`/`$none` quantifiers:
  join or filter in the base query.
- JSONB key paths and polymorphic `a~b` columns as fields: map a field to an expression
  (`data->>'theme'`, `COALESCE(a.name, b.name)`).
- Databases other than Postgres.
