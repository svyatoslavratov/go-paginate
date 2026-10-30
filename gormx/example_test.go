package gormx_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/gormx"
)

type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// usersEndpoint is compiled once: a broken config stops the service at startup.
var usersEndpoint = paginate.MustCompile(paginate.Config{
	Fields: []paginate.Field{
		{Name: "createdAt", Column: "users.created_at", Type: paginate.Time, Sortable: true},
		{Name: "name", Column: "users.name", Sortable: true, Searchable: true},
		{Name: "status", Column: "users.status", Type: paginate.Enum("active", "invited"), Filter: []paginate.Operator{paginate.OpIn}},
	},
	DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
	TieBreak:    "users.id",
})

// A list endpoint with net/http and GORM: parse, paginate, answer.
func ExamplePaginate() {
	var db *gorm.DB // opened elsewhere

	handler := func(w http.ResponseWriter, r *http.Request) {
		req, err := paginate.ParseRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		page, err := gormx.Paginate[User](r.Context(), *req, usersEndpoint, db.Model(&User{}).Where("users.tenant_id = ?", 7))
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	}
	_ = handler
}

// writeError answers a client's mistake with its code and field, anything else with a bare 500.
func writeError(w http.ResponseWriter, err error) {
	status := paginate.HTTPStatus(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusBadRequest {
		_ = json.NewEncoder(w).Encode(err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "internal error"})
}

// The builder takes columns from the GORM schema, API names from the DTO and types from the model,
// so a renamed field breaks the build instead of a request.
func ExampleNewBuilder() {
	type BaseModel struct {
		ID        string `gorm:"type:uuid;primaryKey"`
		CreatedAt time.Time
	}
	type Template struct {
		BaseModel
		Title string
		Price int64
	}
	type TemplateDTO struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Price     int64     `json:"price"`
		CreatedAt time.Time `json:"createdAt"`
	}

	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}} // gorm.Open(...) in real code
	var m Template
	var d TemplateDTO
	b := gormx.NewBuilder(db, "templates", &m, &d)
	b.Field(&d.ID, &m.ID).Type(paginate.UUID).Filter()
	b.Field(&d.Title, &m.Title).Sortable().Searchable()
	b.Field(&d.Price, &m.Price).Sortable().Filter(paginate.OpBtw)
	b.Field(&d.CreatedAt, &m.CreatedAt).Sortable()
	b.DefaultSort(&d.CreatedAt, paginate.Desc)
	cfg, err := b.Config()
	if err != nil {
		panic(err)
	}

	for _, field := range cfg.Fields {
		fmt.Println(field.Name, field.Column, field.Type)
	}
	_, err = paginate.Compile(cfg)
	fmt.Println(cfg.TieBreak, err)
	// Output:
	// id templates.id uuid
	// title templates.title text
	// price templates.price int
	// createdAt templates.created_at time
	// templates.id <nil>
}

// ConfigOf reads paginate tags from a GORM model and the columns from its schema.
func ExampleConfigOf() {
	type Product struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name" gorm:"column:title" paginate:"sort;search;filter:$ilike"`
		Price     float64   `json:"price" paginate:"sort;filter:$gte,$lte"`
		CreatedAt time.Time `json:"createdAt" paginate:"sort"`
	}

	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}} // gorm.Open(...) in real code
	cfg, err := gormx.ConfigOf[Product](db, "")
	if err != nil {
		panic(err)
	}
	for _, field := range cfg.Fields {
		fmt.Println(field.Name, field.Column, field.Type, field.Filter)
	}
	fmt.Println(cfg.TieBreak)
	// Output:
	// name products.title text [$eq $ilike]
	// price products.price number [$eq $gte $lte]
	// createdAt products.created_at time []
	// products.id
}
