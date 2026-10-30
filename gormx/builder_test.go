package gormx

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/svyatoslavratov/go-paginate"
)

type BaseModel struct {
	ID        string `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

type audit struct {
	CreatedBy string
	Reason    string
}

type author struct {
	ID   string
	Name string
}

type template struct {
	BaseModel
	Title    string
	Audit    audit `gorm:"embedded;embeddedPrefix:audit_"`
	AuthorID string
	Author   author `gorm:"foreignKey:AuthorID"`
	Draft    string `gorm:"-"`
}

type authorDTO struct {
	Name string `json:"name"`
}

type pageDTO struct {
	Total int `json:"total"`
}

type templateDTO struct {
	pageDTO
	ID      string    `json:"id"`
	Title   string    `json:"title,omitempty"`
	Created time.Time `json:"createdAt"`
	Author  authorDTO `json:"author"`
	Note    string
	Secret  string `json:"-"`
}

type builderModel struct {
	ID        uint `gorm:"primaryKey"`
	Score     float64
	CreatedAt int64
}

type builderCompositeModel struct {
	TenantID uint   `gorm:"primaryKey"`
	Code     string `gorm:"primaryKey"`
	Score    float64
}

type builderDTO struct {
	Score     float64 `json:"score"`
	CreatedAt int64   `json:"createdAt"`
}

// namingDB is all the builder needs: the naming strategy.
func namingDB() *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
}

func buildErr(b *Builder) error {
	_, err := b.Config()
	return err
}

func TestBuilderColumns(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	tests := []struct {
		name  string
		field any
		want  string
	}{
		// The first field of an embedded struct shares its address with the struct, so the address
		// alone would resolve it to "templates.base_model".
		{name: "id of an embedded base model", field: &model.BaseModel.ID, want: "templates.id"}, //nolint:staticcheck // the explicit selector is the case under test
		{name: "promoted id", field: &model.ID, want: "templates.id"},
		{name: "second field of the base model", field: &model.CreatedAt, want: "templates.created_at"},
		{name: "own field", field: &model.Title, want: "templates.title"},
		{name: "gorm embedded struct with a prefix", field: &model.Audit.Reason, want: "templates.audit_reason"},
		{name: "first field of the gorm embedded struct", field: &model.Audit.CreatedBy, want: "templates.audit_created_by"},
		{name: "foreign key column", field: &model.AuthorID, want: "templates.author_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := NewBuilder(namingDB(), "templates", &model, &dto)
			if got := b.Column(tc.field); got != tc.want {
				t.Fatalf("want %q, got %q (errors: %v)", tc.want, got, buildErr(b))
			}
			if err := buildErr(b); err != nil {
				t.Fatalf("unexpected builder error: %v", err)
			}
		})
	}
}

func TestBuilderRejectsNonColumns(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	var other template
	tests := []struct {
		name  string
		field any
		want  string
	}{
		{name: "relation", field: &model.Author, want: "field Author is not a column"},
		{name: "field of a relation", field: &model.Author.Name, want: "inside Author, which is neither embedded"},
		{name: "ignored field", field: &model.Draft, want: "field Draft is not a column"},
		{name: "field of another value", field: &other.Title, want: "is not a field of"},
		{name: "not a pointer", field: model.Title, want: "is not a pointer to a field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := NewBuilder(namingDB(), "templates", &model, &dto)
			if got := b.Column(tc.field); got != "" {
				t.Fatalf("want no column, got %q", got)
			}
			if err := buildErr(b); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error with %q, got %v", tc.want, err)
			}
		})
	}
}

func TestBuilderJSONNames(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	tests := []struct {
		name    string
		field   any
		want    string
		wantErr string
	}{
		{name: "tag", field: &dto.ID, want: "id"},
		{name: "tag with options", field: &dto.Title, want: "title"},
		{name: "nested struct", field: &dto.Author.Name, want: "author.name"},
		{name: "promoted field of an embedded struct", field: &dto.Total, want: "total"},
		{name: "no tag: the field name, as encoding/json", field: &dto.Note, want: "Note"},
		{name: "not serialized", field: &dto.Secret, wantErr: "is not serialized"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := NewBuilder(namingDB(), "templates", &model, &dto)
			got := b.JSONName(tc.field)
			if tc.wantErr != "" {
				if err := buildErr(b); got != "" || err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error with %q, got %q and %v", tc.wantErr, got, err)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q (errors: %v)", tc.want, got, buildErr(b))
			}
		})
	}
}

func TestBuilderConfig(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	b := NewBuilder(namingDB(), "templates", &model, &dto)
	id := b.Field(&dto.ID, &model.ID)
	b.Field(&dto.Title, &model.Title).Sortable().Searchable().Filter(paginate.OpILike)
	b.Field(&dto.Created, &model.CreatedAt).Type(paginate.Date).Filter(paginate.OpEq, paginate.OpBtw, paginate.OpBtw)
	// A field builder keeps working after more fields are added.
	id.Sortable().Filter()
	b.DefaultSort(&dto.Created, paginate.Desc)
	cfg, err := b.Config()
	if err != nil {
		t.Fatal(err)
	}
	want := []paginate.Field{
		{Name: "id", Column: "templates.id", Type: paginate.Text, Sortable: true, Filter: []paginate.Operator{paginate.OpEq}, NotNull: true},
		{Name: "title", Column: "templates.title", Type: paginate.Text, Sortable: true, Searchable: true, Filter: []paginate.Operator{paginate.OpEq, paginate.OpILike}},
		{Name: "createdAt", Column: "templates.created_at", Type: paginate.Date, Filter: []paginate.Operator{paginate.OpEq, paginate.OpBtw}},
	}
	if !reflect.DeepEqual(cfg.Fields, want) {
		t.Fatalf("fields:\nwant %+v\ngot  %+v", want, cfg.Fields)
	}
	if !reflect.DeepEqual(cfg.DefaultSort, []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}}) || cfg.TieBreak != "templates.id" {
		t.Fatalf("default sort %v, tie-break %q", cfg.DefaultSort, cfg.TieBreak)
	}
	if _, err := paginate.Compile(cfg); err != nil {
		t.Fatalf("the config must compile: %v", err)
	}
}

func TestBuilderSkipsWhatItCannotResolve(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	b := NewBuilder(namingDB(), "templates", &model, &dto)
	b.Field(&dto.Title, &model.Title).Sortable()
	b.Field(&dto.Author.Name, &model.Author).Sortable().Searchable()
	cfg, err := b.Config()
	if err == nil || !strings.Contains(err.Error(), "field Author is not a column") {
		t.Fatalf("want the unresolved field reported, got %v", err)
	}
	if len(cfg.Fields) != 1 || cfg.Fields[0].Name != "title" {
		t.Fatalf("unresolved fields must be left out: %+v", cfg.Fields)
	}
}

func TestBuilderWithoutDB(t *testing.T) {
	t.Parallel()
	var model template
	b := NewBuilder(nil, "templates", &model, nil)
	if got := b.Column(&model.Title); got != "" {
		t.Fatalf("want no column, got %q", got)
	}
	if err := buildErr(b); err == nil || !strings.Contains(err.Error(), "gorm db is required") {
		t.Fatalf("want an error, got %v", err)
	}
}

func TestBuilderTieBreakAndDefaultSortOrder(t *testing.T) {
	t.Parallel()
	t.Run("primary key from schema, default sort keys in call order", func(t *testing.T) {
		t.Parallel()
		var model builderModel
		var dto builderDTO
		b := NewBuilder(namingDB(), "things", &model, &dto)
		b.Field(&dto.Score, &model.Score).Sortable()
		b.Field(&dto.CreatedAt, &model.CreatedAt).Sortable()
		b.DefaultSort(&dto.Score, paginate.Desc).DefaultSort(&dto.CreatedAt, paginate.Asc)
		cfg, err := b.Config()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.TieBreak != "things.id" {
			t.Fatalf("want things.id, got %q", cfg.TieBreak)
		}
		if want := []paginate.Sort{{Field: "score", Order: paginate.Desc}, {Field: "createdAt", Order: paginate.Asc}}; !reflect.DeepEqual(cfg.DefaultSort, want) {
			t.Fatalf("want %v, got %v", want, cfg.DefaultSort)
		}
		if cfg.Fields[0].Type.String() != "number" || cfg.Fields[1].Type.String() != "int" {
			t.Fatalf("types from the model: %v %v", cfg.Fields[0].Type, cfg.Fields[1].Type)
		}
	})
	t.Run("composite primary key: no tie-breaker", func(t *testing.T) {
		t.Parallel()
		var model builderCompositeModel
		var dto builderDTO
		cfg, _ := NewBuilder(namingDB(), "things", &model, &dto).Config()
		if cfg.TieBreak != "" {
			t.Fatalf("want no tie-breaker, got %q", cfg.TieBreak)
		}
	})
	t.Run("explicit override and opt-out", func(t *testing.T) {
		t.Parallel()
		var model builderModel
		var dto builderDTO
		b := NewBuilder(namingDB(), "things", &model, &dto)
		if cfg, _ := b.TieBreak(&model.CreatedAt).Config(); cfg.TieBreak != "things.created_at" {
			t.Fatalf("want things.created_at, got %q", cfg.TieBreak)
		}
		if cfg, _ := b.WithoutTieBreak().Config(); cfg.TieBreak != "" {
			t.Fatalf("want empty, got %q", cfg.TieBreak)
		}
	})
}
