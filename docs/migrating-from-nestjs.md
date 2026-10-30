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
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Color        string     `json:"color"`
	Age          int        `json:"age"`
	LastVetVisit *time.Time `json:"lastVetVisit"`
	CreatedAt    time.Time  `json:"createdAt"`
}
```

nestjs-paginate names columns by entity keys. Here clients name API fields, and each field of the
config maps its name to an SQL column (`Field.Name`, `Field.Column`), so the API and the schema can
differ.

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
		writeError(w, err)
		return
	}
	page, err := h.cats.FindAll(r.Context(), *query)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// writeError answers a client's mistake with 400 and {"code", "field", "message"}, where nestjs
// throws a BadRequestException, and anything else with a bare 500.
func writeError(w http.ResponseWriter, err error) {
	if status := paginate.HTTPStatus(err); status == http.StatusBadRequest {
		writeJSON(w, status, err)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
}
```

With fasthttp, use `fasthttpx.ParseRequest(ctx)`. [Errors](errors.md) lists the codes.

## paginate(query, repo, config)

```ts
const catsConfig: PaginateConfig<CatEntity> = {
  sortableColumns: ['id', 'name', 'color', 'age'],
  nullSort: 'last',
  defaultSortBy: [['id', 'DESC']],
  searchableColumns: ['name', 'color', 'age'],
  filterableColumns: {
    name: [FilterOperator.EQ, FilterSuffix.NOT],
    age: true,
  },
}

@Injectable()
export class CatsService {
  constructor(@InjectRepository(CatEntity) private readonly catsRepository: Repository<CatEntity>) {}

  public findAll(query: PaginateQuery): Promise<Paginated<CatEntity>> {
    return paginate(query, this.catsRepository, catsConfig)
  }
}
```

```go
type CatsService struct {
	db       *gorm.DB
	endpoint *paginate.Endpoint
}

func NewCatsService(db *gorm.DB) *CatsService {
	cfg := paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "cats.id", Type: paginate.Int, Sortable: true},
			{Name: "name", Column: "cats.name", Sortable: true, Searchable: true,
				Filter: []paginate.Operator{paginate.OpEq}}, // $not goes with every allowed operator
			{Name: "color", Column: "cats.color", Sortable: true, Searchable: true},
			{Name: "age", Column: "cats.age", Type: paginate.Int, Sortable: true, Searchable: true,
				Filter: paginate.AllOperators}, // true
		},
		NullSort:    paginate.NullsLast,                                   // nullSort: 'last'
		DefaultSort: []paginate.Sort{{Field: "id", Order: paginate.Desc}}, // defaultSortBy
		TieBreak:    "cats.id",                                            // a unique column ends every sort
	}
	// Compiled once: a broken config stops the service at startup, not a request.
	return &CatsService{db: db, endpoint: paginate.MustCompile(cfg)}
}

func (s *CatsService) FindAll(ctx context.Context, query paginate.Request) (*paginate.Response[Cat], error) {
	return gormx.Paginate[Cat](ctx, query, s.endpoint, s.db.Model(&Cat{}))
}
```

A field allows only what it says: `Sortable`, `Searchable`, `Filter`. A nil `Filter` is not
filterable, `[]paginate.Operator{paginate.OpEq}` allows `$eq` only (an empty list in
nestjs-paginate), `paginate.AllOperators` every operator that fits the field's type (`true`). `$not`
goes with every allowed operator, listed or not. `Type` checks filter values before the query:
`?filter.age=$gte:old` is a 400 that names `age`. The sections below set more fields of `cfg` before
it is compiled; [Configuration](configuration.md) describes them all.

Or put the API on the model with tags:

```go
type Cat struct {
	ID           int64      `json:"id" paginate:"sort"`
	Name         string     `json:"name" paginate:"sort;search;filter"`
	Color        string     `json:"color" paginate:"sort;search"`
	Age          int        `json:"age" paginate:"sort;search;filter:*"`
	LastVetVisit *time.Time `json:"lastVetVisit"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func NewCatsService(db *gorm.DB) (*CatsService, error) {
	cfg, err := gormx.ConfigOf[Cat](db, "") // columns from the GORM schema, the primary key as TieBreak
	if err != nil {
		return nil, err
	}
	cfg.NullSort = paginate.NullsLast
	cfg.DefaultSort = []paginate.Sort{{Field: "id", Order: paginate.Desc}}
	endpoint, err := paginate.Compile(cfg)
	if err != nil {
		return nil, err
	}
	return &CatsService{db: db, endpoint: endpoint}, nil
}
```

`filter` allows `$eq`, `filter:$in,$btw` these operators too, `filter:*` every operator. The Go
types give the field types; `type:` and `enum:` set them, `column:` and `name:` set the column and
the API name. `paginate.ConfigOf[Cat]("cats")` reads the same tags without GORM; there the field
tagged `unique` is the tie-breaker. When the model and the API type differ, `gormx.NewBuilder` builds
the config from pointers to model and DTO fields, see [GORM: tags and the builder](use-cases/gorm-builder.md).

## The answer in nestjs-paginate's shape

```ts
// Paginated<CatEntity>: { data, meta: { ..., sortBy: [['id', 'DESC']] }, links: { ... } }
```

```go
cfg.Format = paginate.FormatNestJS
cfg.LinksBaseURL = "https://api.example.com" // origin; empty gives relative links (relativePath: true)
```

```http
GET /cats?limit=5&page=2&sortBy=color:DESC&search=i&filter.age=$gte:3
```

```json
{
  "data": [{ "id": 4, "name": "George", "color": "white", "age": 3, "lastVetVisit": null, "createdAt": "2022-12-01T10:00:00Z" }],
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

The default format, without `FormatNestJS`, has the same `meta.sortBy` pairs, no links, and adds
`hasNextPage`, `hasPreviousPage` and `paginationMode`.

A handler that maps rows to DTOs keeps the meta and the links with `paginate.Map`:

```go
type CatDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

writeJSON(w, http.StatusOK, paginate.Map(page, func(c Cat) CatDTO {
	return CatDTO{ID: c.ID, Name: c.Name}
}))
```

A handler that answers with its own generated types reads the same data from `page.Meta.NestJS()`
and `page.Meta.Links()`.

## Query builder

```ts
const queryBuilder = repo
  .createQueryBuilder('cats')
  .leftJoinAndSelect('cats.owner', 'owner')
  .where('cats.owner = :ownerId', { ownerId })

const result = await paginate<CatEntity>(query, queryBuilder, catsConfig)
```

```go
base := s.db.Model(&Cat{}).
	Joins("LEFT JOIN owners AS owner ON owner.id = cats.owner_id").
	Where("cats.owner_id = ?", ownerID)

page, err := gormx.Paginate[Cat](ctx, query, s.endpoint, base)
```

With squirrel and pgx, where `scanCats` reads the rows (see [squirrel + pgx](use-cases/squirrel-pgx.md)):

```go
qb := squirrel.Select("cats.id", "cats.name", "cats.color", "cats.age").
	From("cats").
	LeftJoin("owners AS owner ON owner.id = cats.owner_id").
	Where(squirrel.Eq{"cats.owner_id": ownerID})

page, err := squirrelx.Paginate(ctx, query, s.endpoint, pool, scanCats, qb)
```

## where

```ts
paginate(query, repo, { ...catsConfig, where: { color: 'black' } })
```

```go
gormx.Paginate[Cat](ctx, query, s.endpoint, s.db.Model(&Cat{}).Where("cats.color = ?", "black"))
```

## Relations

```ts
// CatEntity has @OneToMany(() => CatToyEntity, (toy) => toy.cat) toys: CatToyEntity[]
const config: PaginateConfig<CatEntity> = {
  relations: { toys: true },
  sortableColumns: ['id', 'name', 'toys.name'],
  filterableColumns: { 'toys.name': [FilterOperator.IN] },
}
// ?filter.toys.name=$in:Mouse,String
```

A to-one relation is a join and a field that reads the joined column:

```go
base := s.db.Model(&Cat{}).Joins("JOIN homes AS home ON home.id = cats.home_id")
```

```go
// ?filter.home.name=$eq:House; every base query of the endpoint must join home.
cfg.Fields = append(cfg.Fields, paginate.Field{Name: "home.name", Column: "home.name", Filter: []paginate.Operator{paginate.OpEq}})
```

A to-many relation (`toys`) would repeat cats per toy in a join. Filter by it with `EXISTS` in the
base query, from your own parameter:

```go
base := s.db.Model(&Cat{}).Where(
	"EXISTS (SELECT 1 FROM toys WHERE toys.cat_id = cats.id AND toys.name IN ?)", toyNames)
```

To load the toys into the cats, as `relations` does, give `Cat` a `Toys []Toy` field and preload it
on the base query: `s.db.Model(&Cat{}).Preload("Toys")`. Preloads run as separate queries, so the
page still counts cats. See [joins, aliases, GROUP BY](use-cases/joins-and-aliases.md).

## Filter expressions

Same syntax:

```http
GET /cats?filter=(color=$eq:black OR color=$eq:white) AND NOT name=$eq:Leche
GET /cats?filter=name=$eq:"Milo \"the cat\""
```

```ts
const config: PaginateConfig<CatEntity> = {
  sortableColumns: ['id'],
  filterableColumns: { color: true, name: true },
  filterExpressionMaxComplexity: 50,
}
```

```go
cfg.MaxFilterExpressionComplexity = 50
```

Terms are checked against the fields' `Filter` lists like `filter.<field>`, and an unknown field or
an operator outside the list is a 400 whatever `Lenient` says.

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
const config: PaginateConfig<CatEntity> = { ...catsConfig, maxLimit: -1 } // allow limit=-1
```

```go
cfg.AllowDisabledPagination = true // allow limit=-1 and paginationMode=disabled
```

`maxLimit: -1` also lifts the cap on `limit`; `MaxLimit` still caps it here. Server code that needs
every row (an export) sets the mode itself and needs no option:

```go
query.Mode = paginate.ModeDisabled
```

## Counts

```ts
const config: PaginateConfig<CatEntity> = { ...catsConfig, optimizedCount: true } // a lighter COUNT
```

There is no hook to rewrite the count: gormx counts with GORM's `Count`, squirrelx wraps the query
in `SELECT COUNT(*) FROM (...)`. Instead, choose how to count:

```go
cfg.Count = paginate.CountEstimate // the planner's estimate from EXPLAIN instead of COUNT(*)
cfg.ExactCountBelow = 1000         // an estimate below 1000 is replaced by an exact count
cfg.ParallelCount = true           // the count and the page at once, on two connections
```

`paginate.CountNone` counts nothing: the executor reads one row more than the page to tell whether
a next page exists, and the answer has no totals and no `last` link. nestjs-paginate always counts
pages by number. See [Pages and counts](pagination.md).

## Cursor pagination

```ts
const config: PaginateConfig<CatEntity> = {
  sortableColumns: ['id', 'lastVetVisit'],
  paginationType: PaginationType.CURSOR,
}
// GET /cats?limit=5&sortBy=lastVetVisit:ASC&cursor=V998328469600000
```

```go
cfg.Fields = append(cfg.Fields, paginate.Field{Name: "lastVetVisit", Column: "cats.last_vet_visit", Type: paginate.Time, Sortable: true})
cfg.Pagination = paginate.CursorPagination // needs TieBreak: Compile fails without it
```

The meta and the links of a page, with `FormatNestJS`:

```json
{
  "meta": {
    "itemsPerPage": 5,
    "sortBy": [["lastVetVisit", "ASC"]],
    "cursor": "WyJuIiwibGFzdFZldFZpc2l0OkFTQyxpZDpBU0MiLCIyMDIyLTEyLTIwVDEwOjAwOjAwWiIsMl0"
  },
  "links": {
    "previous": "https://api.example.com/cats?limit=5&sortBy=lastVetVisit:ASC&cursor=WyJwIiwibGFzdFZldFZpc2l0OkFTQyxpZDpBU0MiLCIyMDIyLTEyLTIxVDEwOjAwOjAwWiIsM10",
    "current": "https://api.example.com/cats?limit=5&sortBy=lastVetVisit:ASC&cursor=WyJuIiwibGFzdFZldFZpc2l0OkFTQyxpZDpBU0MiLCIyMDIyLTEyLTIwVDEwOjAwOjAwWiIsMl0",
    "next": "https://api.example.com/cats?limit=5&sortBy=lastVetVisit:ASC&cursor=WyJuIiwibGFzdFZldFZpc2l0OkFTQyxpZDpBU0MiLCIyMDIyLTEyLTI1VDEwOjAwOjAwWiIsN10"
  }
}
```

The shape is nestjs-paginate's. What differs:

- The cursor is opaque: base64url JSON with the values of the boundary row and the sort it was made
  for, here `["n","lastVetVisit:ASC,id:ASC","2022-12-20T10:00:00Z",2]`. A cursor sent with another
  `sortBy` is a 400 (`invalid_cursor`).
- The `previous` link keeps `sortBy=lastVetVisit:ASC`, where nestjs-paginate reverses it to `DESC`:
  the cursor carries the direction, and the previous page comes in the same order.
- `next` is linked only when more rows exist, and `previous` only when rows exist before the page.
  nestjs-paginate links both whenever the page has rows.
- The next page is found with `WHERE` on the sort columns, not on a string built for every row. Any
  sortable field works, text included, and NULL values go where `NullSort` puts them.
- `TieBreak` ends every sort, so clients need not add a unique column to `sortBy`.
- The rows must carry the sort fields, by JSON name or by column name: the cursors are made from
  them.

## Soft deletes

```ts
const config: PaginateConfig<CatEntity> = { ...catsConfig, allowWithDeletedInQuery: true } // ?withDeleted=true
```

```go
cfg.AllowWithDeletedInQuery = true // GORM: the request runs Unscoped
```

`withDeleted: true`, deleted rows on every request, is `Unscoped()` on the base query:
`s.db.Unscoped().Model(&Cat{})`.

## Multi-word search and searchBy

```ts
const config: PaginateConfig<CatEntity> = { ...catsConfig, multiWordSearch: true, ignoreSearchByInQueryParam: true }
```

```go
cfg.MultiWordSearch = true
cfg.IgnoreSearchByInQueryParam = true
```

## Invalid input

```ts
const config: PaginateConfig<CatEntity> = { ...catsConfig, throwOnInvalidFilter: true } // 400 for invalid filter.<field>
```

Here that is the default, and it covers `sortBy` and `searchBy` too, which nestjs-paginate drops when
they name fields its config does not list. To drop unknown fields and operators that are not
allowed, as nestjs-paginate does by default:

```go
cfg.Lenient = true
```

Malformed values, values over the caps, values that do not fit the field's `Type`, filter
expressions and cursors are a 400 either way. The body says what is wrong and where:

```http
GET /cats?filter.age=$gte:old
```

```json
{"code":"invalid_value","field":"age","message":"invalid value \"old\" for age: must be an integer"}
```

nestjs-paginate answers its 400s with Nest's
`{"statusCode": 400, "message": "...", "error": "Bad Request"}`, and a value the column rejects fails
in the database with a 500. See [Errors](errors.md).

## Global defaults

```ts
updateGlobalConfig({ defaultLimit: 20, defaultMaxLimit: 100, defaultOrigin: 'https://api.example.com' })
```

```go
// Every endpoint of the service is compiled through one function.
func compile(cfg paginate.Config) (*paginate.Endpoint, error) {
	cfg.DefaultLimit, cfg.MaxLimit = 20, 100
	cfg.Format = paginate.FormatNestJS
	cfg.LinksBaseURL = "https://api.example.com"
	return paginate.Compile(cfg)
}
```

## Swagger

```ts
@PaginatedSwaggerDocs(UserDto, USER_PAGINATION_CONFIG)
```

Describe the parameters and the answer in the OpenAPI spec, see [OpenAPI](use-cases/openapi.md).
`Endpoint.Fields()` returns the fields with their types and operators, to generate that description
or a filter UI from.

## Not supported

- `select=`: the base query's `Select` and the row type decide the fields.
- `relations` in the config, to-many `EXISTS` filters and the `$any`/`$all`/`$none` quantifiers:
  join, preload or filter in the base query.
- JSONB key paths and polymorphic `a~b` columns as fields: map a field to an expression
  (`data->>'theme'`, `COALESCE(a.name, b.name)`).
- `$lte:YYYY-MM-DD` up to the end of the day: use `$lt` with the next day.
- Databases other than Postgres.
