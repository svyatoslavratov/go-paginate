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

func buildErr(b *ConfigBuilder) error {
	_, err := b.Build()
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
			b := NewConfigBuilder(namingDB(), "templates", &model, &dto)
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
			b := NewConfigBuilder(namingDB(), "templates", &model, &dto)
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
			b := NewConfigBuilder(namingDB(), "templates", &model, &dto)
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

func TestBuilderBuild(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	cfg, err := NewConfigBuilder(namingDB(), "templates", &model, &dto).
		AddSortable(&model.ID, &model.Title).
		AddSearchable(&model.Title).
		AddFilterable(&model.ID).
		AddFilterable(&model.Title, paginate.OpILike).
		MapField(&dto.ID, &model.ID).
		MapField(&dto.Title, &model.Title).
		AddDefaultSort(&model.CreatedAt, paginate.Desc).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.SortableColumns, []string{"templates.id", "templates.title"}) {
		t.Fatalf("sortable: %v", cfg.SortableColumns)
	}
	if !reflect.DeepEqual(cfg.FieldColumns, map[string]string{"id": "templates.id", "title": "templates.title"}) {
		t.Fatalf("fields: %v", cfg.FieldColumns)
	}
	// No operators means $eq only, not "not filterable".
	if operators, ok := cfg.FilterableColumns["templates.id"]; !ok || operators == nil || len(operators) != 0 {
		t.Fatalf("filterable id: %v %v", operators, ok)
	}
	if cfg.TieBreakColumn != "templates.id" {
		t.Fatalf("tie-break: %q", cfg.TieBreakColumn)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestBuilderSkipsWhatItCannotResolve(t *testing.T) {
	t.Parallel()
	var model template
	var dto templateDTO
	cfg, err := NewConfigBuilder(namingDB(), "templates", &model, &dto).
		AddSortable(&model.Title, &model.Author).
		MapField(&dto.Title, &model.Title).
		Build()
	if err == nil || !strings.Contains(err.Error(), "field Author is not a column") {
		t.Fatalf("want the unresolved field reported, got %v", err)
	}
	if !reflect.DeepEqual(cfg.SortableColumns, []string{"templates.title"}) {
		t.Fatalf("unresolved fields must be left out: %v", cfg.SortableColumns)
	}
}

func TestBuilderWithoutDB(t *testing.T) {
	t.Parallel()
	var model template
	b := NewConfigBuilder(nil, "templates", &model, nil)
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
		cfg, err := NewConfigBuilder(namingDB(), "things", &model, &dto).
			AddSortable(&model.Score, &model.CreatedAt).
			AddDefaultSort(&model.Score, paginate.Desc).
			AddDefaultSort(&model.CreatedAt, paginate.Desc).
			AddDefaultSort(&model.Score, paginate.Asc).
			Build()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.TieBreakColumn != "things.id" {
			t.Fatalf("want things.id, got %q", cfg.TieBreakColumn)
		}
		if want := []string{"things.score", "things.created_at"}; !reflect.DeepEqual(cfg.DefaultSortKeys, want) {
			t.Fatalf("want %v, got %v", want, cfg.DefaultSortKeys)
		}
		if cfg.DefaultSortBy["things.score"] != paginate.Asc {
			t.Fatalf("a repeated AddDefaultSort must overwrite the order, got %v", cfg.DefaultSortBy)
		}
	})
	t.Run("composite primary key: no tie-breaker", func(t *testing.T) {
		t.Parallel()
		var model builderCompositeModel
		var dto builderDTO
		cfg, _ := NewConfigBuilder(namingDB(), "things", &model, &dto).AddSortable(&model.Score).Build()
		if cfg.TieBreakColumn != "" {
			t.Fatalf("want no tie-breaker, got %q", cfg.TieBreakColumn)
		}
	})
	t.Run("explicit override and opt-out", func(t *testing.T) {
		t.Parallel()
		var model builderModel
		var dto builderDTO
		builder := NewConfigBuilder(namingDB(), "things", &model, &dto).AddSortable(&model.Score)
		if cfg, _ := builder.WithTieBreak(&model.CreatedAt).Build(); cfg.TieBreakColumn != "things.created_at" {
			t.Fatalf("want things.created_at, got %q", cfg.TieBreakColumn)
		}
		if cfg, _ := builder.WithoutTieBreak().Build(); cfg.TieBreakColumn != "" {
			t.Fatalf("want empty, got %q", cfg.TieBreakColumn)
		}
	})
}
