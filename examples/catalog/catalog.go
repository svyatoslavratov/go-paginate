// Package catalog is the go-paginate demo: a products table, the config of its list endpoint, and
// a description of the fields for the demo page. The server (cmd/server) and the browser build
// (cmd/wasm) share it, so both answer the same way.
package catalog

import (
	_ "embed"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/svyatoslavratov/go-paginate"
)

// SeedSQL creates and fills the products table; running it again changes nothing.
//
//go:embed seed.sql
var SeedSQL string

// Path is the path of the list endpoint; links in the answers start with it.
const Path = "/api/products"

// Product is a row of the products table; its JSON names are the API fields.
type Product struct {
	ID             int        `json:"id" gorm:"primaryKey"`
	SKU            string     `json:"sku" gorm:"column:sku"`
	Name           string     `json:"name"`
	Brand          string     `json:"brand"`
	Category       string     `json:"category"`
	Price          float64    `json:"price"`
	Rating         *float64   `json:"rating"`
	Stock          int        `json:"stock"`
	Tags           Tags       `json:"tags" gorm:"type:text[]"`
	ReleasedAt     time.Time  `json:"releasedAt"`
	DiscontinuedAt *time.Time `json:"discontinuedAt"`
}

// TableName names the table for GORM.
func (Product) TableName() string { return "products" }

// Tags reads a text[] column.
type Tags []string

// Scan reads the text form of a Postgres array.
func (t *Tags) Scan(src any) error {
	var tags []string
	if err := pgtype.NewMap().SQLScanner(&tags).Scan(src); err != nil {
		return err
	}
	*t = tags
	return nil
}

// BrowserColumns are the columns the browser build selects, named as the JSON of Product, so its
// rows look like the server's.
var BrowserColumns = []string{
	`products.id AS "id"`,
	`products.sku AS "sku"`,
	`products.name AS "name"`,
	`products.brand AS "brand"`,
	`products.category AS "category"`,
	`products.price::float8 AS "price"`,
	`products.rating::float8 AS "rating"`,
	`products.stock AS "stock"`,
	`products.tags AS "tags"`,
	`products.released_at AS "releasedAt"`,
	`products.discontinued_at AS "discontinuedAt"`,
}

// Config is the config of the list endpoint: every operator where it makes sense, the
// nestjs-paginate answer, and every row on request (the table is small).
func Config() paginate.Config {
	cfg := paginate.NewConfig("products",
		[]string{
			"products.released_at", "products.id", "products.name", "products.brand", "products.category",
			"products.price", "products.rating", "products.stock",
		},
		map[string]string{
			"id":             "products.id",
			"sku":            "products.sku",
			"name":           "products.name",
			"brand":          "products.brand",
			"category":       "products.category",
			"price":          "products.price",
			"rating":         "products.rating",
			"stock":          "products.stock",
			"tags":           "products.tags",
			"releasedAt":     "products.released_at",
			"discontinuedAt": "products.discontinued_at",
		})
	cfg.SearchableColumns = []string{"products.name", "products.brand", "products.category"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"products.id":              {paginate.OpIn, paginate.OpGt, paginate.OpGte, paginate.OpLt, paginate.OpLte, paginate.OpBtw},
		"products.sku":             {},
		"products.name":            {paginate.OpILike, paginate.OpSw, paginate.OpIn},
		"products.brand":           {paginate.OpIn, paginate.OpSw, paginate.OpILike},
		"products.category":        {paginate.OpIn},
		"products.price":           {paginate.OpGt, paginate.OpGte, paginate.OpLt, paginate.OpLte, paginate.OpBtw},
		"products.rating":          {paginate.OpGte, paginate.OpLte, paginate.OpBtw, paginate.OpNull},
		"products.stock":           {paginate.OpGt, paginate.OpLt, paginate.OpBtw},
		"products.tags":            {paginate.OpContains},
		"products.released_at":     {paginate.OpGte, paginate.OpLt, paginate.OpBtw},
		"products.discontinued_at": {paginate.OpNull},
	}
	cfg.DefaultSortBy = paginate.SortBy{"products.released_at": paginate.Desc}
	cfg.TieBreakColumn = "products.id"
	cfg.NullSort = paginate.NullsLast
	cfg.MultiWordSearch = true
	cfg.AllowDisabledPagination = true
	cfg.Format = paginate.FormatNestJS
	return *cfg
}

// ConfigFor returns the config with the answer format the demo page asked for: "default" for this
// package's own shape, anything else for nestjs-paginate's.
func ConfigFor(format string) paginate.Config {
	cfg := Config()
	if format == "default" {
		cfg.Format = paginate.FormatDefault
	}
	return cfg
}

// Field describes an API field for the demo page.
type Field struct {
	Name       string              `json:"name"`
	Label      string              `json:"label"`
	Type       string              `json:"type"`
	Sortable   bool                `json:"sortable"`
	Searchable bool                `json:"searchable"`
	Filterable bool                `json:"filterable"`
	Operators  []paginate.Operator `json:"operators"`
	Example    string              `json:"example"`
}

var fields = []Field{
	{Name: "id", Label: "ID", Type: "number", Example: "1,2,3"},
	{Name: "sku", Label: "SKU", Type: "uuid", Example: "8402e99c-5b52-5201-6079-4615a8161683"},
	{Name: "name", Label: "Name", Type: "text", Example: "lamp"},
	{Name: "brand", Label: "Brand", Type: "text", Example: "Lumen"},
	{Name: "category", Label: "Category", Type: "text", Example: "lighting,decor"},
	{Name: "price", Label: "Price", Type: "number", Example: "10,100"},
	{Name: "rating", Label: "Rating", Type: "number", Example: "4"},
	{Name: "stock", Label: "Stock", Type: "number", Example: "10"},
	{Name: "tags", Label: "Tags", Type: "tags", Example: "sale,new"},
	{Name: "releasedAt", Label: "Released", Type: "date", Example: "2024-01-01"},
	{Name: "discontinuedAt", Label: "Discontinued", Type: "date"},
}

// Fields describes the fields as Config allows them.
func Fields() []Field {
	cfg := Config()
	described := make([]Field, 0, len(fields))
	for _, field := range fields {
		column := cfg.FieldColumns[field.Name]
		field.Sortable = slices.Contains(cfg.SortableColumns, column)
		field.Searchable = slices.Contains(cfg.SearchableColumns, column)
		if operators, ok := cfg.FilterableColumns[column]; ok {
			field.Filterable = true
			field.Operators = append([]paginate.Operator{paginate.OpEq}, operators...)
		}
		described = append(described, field)
	}
	return described
}
