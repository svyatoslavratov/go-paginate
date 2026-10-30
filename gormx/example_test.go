package gormx_test

import (
	"encoding/json"
	"errors"
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

// A list endpoint with net/http and GORM: parse, paginate, answer.
func ExamplePaginate() {
	var db *gorm.DB // opened elsewhere

	cfg := paginate.NewConfig("users", []string{"users.created_at", "users.name"}, map[string]string{
		"createdAt": "users.created_at",
		"name":      "users.name",
		"status":    "users.status",
	})
	cfg.FilterableColumns = paginate.FilterableColumns{"users.status": {paginate.OpIn}}
	cfg.DefaultSortBy = paginate.SortBy{"users.created_at": paginate.Desc}
	cfg.TieBreakColumn = "users.id"

	handler := func(w http.ResponseWriter, r *http.Request) {
		req, err := paginate.ParseRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		page, err := gormx.Paginate[User](r.Context(), *req, *cfg, db.Model(&User{}).Where("users.tenant_id = ?", 7))
		switch {
		case errors.Is(err, paginate.ErrInvalidRequest):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(page)
		}
	}
	_ = handler
}

// The builder takes columns from the GORM schema and API names from the DTO, so a renamed field
// fails to compile instead of failing a request.
func ExampleNewConfigBuilder() {
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
	cfg, err := gormx.NewConfigBuilder(db, "templates", &m, &d).
		AddSortable(&m.CreatedAt, &m.Title, &m.Price).
		AddSearchable(&m.Title).
		AddFilterable(&m.ID).
		AddFilterable(&m.Price, paginate.OpBtw).
		MapField(&d.ID, &m.ID).
		MapField(&d.Title, &m.Title).
		MapField(&d.Price, &m.Price).
		MapField(&d.CreatedAt, &m.CreatedAt).
		AddDefaultSort(&m.CreatedAt, paginate.Desc).
		Build()
	if err != nil {
		panic(err)
	}

	fmt.Println(cfg.SortableColumns)
	fmt.Println(cfg.FieldColumns["id"], cfg.TieBreakColumn)
	fmt.Println(cfg.Validate())
	// Output:
	// [templates.created_at templates.title templates.price]
	// templates.id templates.id
	// <nil>
}
