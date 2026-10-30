// Package catalog is the go-paginate demo: a products table, the endpoints of its list, and a
// description of the fields for the demo page. The server (cmd/server) and the browser build
// (cmd/wasm) share it, so both answer the same way.
package catalog

import (
	_ "embed"
	"net/url"
	"strings"
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

// Product is a row of the products table. Its paginate tags are the API of the list: what clients
// may sort, search and filter by, and with which operators.
type Product struct {
	ID             int        `json:"id" gorm:"primaryKey" paginate:"unique;sort;filter:$in,$gt,$gte,$lt,$lte,$btw"`
	SKU            string     `json:"sku" gorm:"column:sku" paginate:"type:uuid;filter;notnull"`
	Name           string     `json:"name" paginate:"sort;search;filter:$ilike,$sw,$in;notnull"`
	Brand          string     `json:"brand" paginate:"sort;search;filter:$in,$sw,$ilike;notnull"`
	Category       string     `json:"category" paginate:"sort;search;filter:$in;enum:lighting,seating,tables,storage,decor;notnull"`
	Price          float64    `json:"price" paginate:"sort;filter:$gt,$gte,$lt,$lte,$btw;notnull"`
	Rating         *float64   `json:"rating" paginate:"sort;filter:$gte,$lte,$btw,$null"`
	Stock          int        `json:"stock" paginate:"sort;filter:$gt,$lt,$btw;notnull"`
	Tags           Tags       `json:"tags" gorm:"type:text[]" paginate:"filter:$contains;enum:sale,new,eco,bestseller,limited"`
	ReleasedAt     time.Time  `json:"releasedAt" paginate:"sort;filter:$gte,$lt,$btw;notnull"`
	DiscontinuedAt *time.Time `json:"discontinuedAt" paginate:"filter:$null"`
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

// Config is the config of the list: the fields from the tags of Product, NULL values last, every
// row on request (the table is small), the nestjs-paginate answer. Estimates of fewer than 100 rows
// are counted exactly.
func Config() paginate.Config {
	cfg, err := paginate.ConfigOf[Product]("products")
	if err != nil {
		panic(err)
	}
	cfg.DefaultSort = []paginate.Sort{{Field: "releasedAt", Order: paginate.Desc}}
	cfg.NullSort = paginate.NullsLast
	cfg.MultiWordSearch = true
	cfg.AllowDisabledPagination = true
	cfg.ExactCountBelow = 100
	cfg.Format = paginate.FormatNestJS
	return cfg
}

// Options pick the endpoint the demo page talks to, through its own query parameters:
// format=default, pages=cursor, count=estimate or count=none.
type Options struct {
	Format     paginate.Format
	Pagination paginate.Pagination
	Count      paginate.Count
}

// OptionsOf reads the options of a query.
func OptionsOf(query url.Values) Options {
	options := Options{Format: paginate.FormatNestJS}
	if query.Get("format") == "default" {
		options.Format = paginate.FormatDefault
	}
	if query.Get("pages") == "cursor" {
		options.Pagination = paginate.CursorPagination
	}
	switch query.Get("count") {
	case "estimate":
		options.Count = paginate.CountEstimate
	case "none":
		options.Count = paginate.CountNone
	}
	return options
}

// endpoints are compiled once, one per combination of options.
var endpoints = func() map[Options]*paginate.Endpoint {
	compiled := make(map[Options]*paginate.Endpoint)
	for _, format := range []paginate.Format{paginate.FormatNestJS, paginate.FormatDefault} {
		for _, pagination := range []paginate.Pagination{paginate.OffsetPagination, paginate.CursorPagination} {
			for _, count := range []paginate.Count{paginate.CountExact, paginate.CountEstimate, paginate.CountNone} {
				cfg := Config()
				cfg.Format, cfg.Pagination, cfg.Count = format, pagination, count
				compiled[Options{format, pagination, count}] = paginate.MustCompile(cfg)
			}
		}
	}
	return compiled
}()

// Endpoint returns the endpoint for the options.
func Endpoint(options Options) *paginate.Endpoint {
	return endpoints[options]
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
	Values     []string            `json:"values,omitempty"`
	Example    string              `json:"example"`
}

var labels = map[string][2]string{
	"id":             {"ID", "1,2,3"},
	"sku":            {"SKU", "8402e99c-5b52-5201-6079-4615a8161683"},
	"name":           {"Name", "lamp"},
	"brand":          {"Brand", "Lumen"},
	"category":       {"Category", "lighting,decor"},
	"price":          {"Price", "10,100"},
	"rating":         {"Rating", "4"},
	"stock":          {"Stock", "10"},
	"tags":           {"Tags", "sale,new"},
	"releasedAt":     {"Released", "2024-01-01"},
	"discontinuedAt": {"Discontinued", ""},
}

// Fields describes the fields of the endpoint for the page: what the controls offer.
func Fields() []Field {
	fields := Endpoint(Options{Format: paginate.FormatNestJS}).Fields()
	described := make([]Field, 0, len(fields))
	for _, field := range fields {
		values := field.Type.Values()
		if field.Type.IsArray() {
			values = field.Type.Elem().Values()
		}
		described = append(described, Field{
			Name:       field.Name,
			Label:      labels[field.Name][0],
			Type:       pageType(field.Type),
			Sortable:   field.Sortable,
			Searchable: field.Searchable,
			Filterable: field.Filter != nil,
			Operators:  field.Filter,
			Values:     values,
			Example:    labels[field.Name][1],
		})
	}
	return described
}

// pageType is how the page shows a field: number, uuid, date, tags or text.
func pageType(t paginate.Type) string {
	switch name := t.String(); {
	case name == "int" || name == "number":
		return "number"
	case name == "uuid":
		return "uuid"
	case name == "time" || name == "date":
		return "date"
	case strings.HasPrefix(name, "array"):
		return "tags"
	}
	return "text"
}
