package gormx

import (
	"reflect"
	"testing"
	"time"

	"github.com/svyatoslavratov/go-paginate"
)

type TaggedBase struct {
	ID        string    `json:"id" gorm:"type:uuid;primaryKey" paginate:"filter;type:uuid"`
	CreatedAt time.Time `json:"createdAt" paginate:"sort;filter:$btw"`
}

type taggedAudit struct {
	Reason string `json:"reason" paginate:"search"`
}

type taggedTemplate struct {
	TaggedBase
	Title  string      `json:"title" gorm:"column:headline" paginate:"sort;search;filter:$ilike"`
	Score  float64     `json:"score" paginate:"sort"`
	Audit  taggedAudit `json:"audit" gorm:"embedded;embeddedPrefix:audit_"`
	Notes  string      `json:"notes"`
	Hidden string      `json:"-" gorm:"-"`
}

func TestConfigOf(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigOf[taggedTemplate](namingDB(), "t")
	if err != nil {
		t.Fatal(err)
	}
	want := []paginate.Field{
		{Name: "id", Column: "t.id", Type: paginate.UUID, Filter: []paginate.Operator{paginate.OpEq}, NotNull: true},
		{Name: "createdAt", Column: "t.created_at", Type: paginate.Time, Sortable: true, Filter: []paginate.Operator{paginate.OpEq, paginate.OpBtw}},
		{Name: "title", Column: "t.headline", Type: paginate.Text, Sortable: true, Searchable: true, Filter: []paginate.Operator{paginate.OpEq, paginate.OpILike}},
		{Name: "score", Column: "t.score", Type: paginate.Number, Sortable: true},
		{Name: "audit.reason", Column: "t.audit_reason", Type: paginate.Text, Searchable: true},
	}
	if !reflect.DeepEqual(cfg.Fields, want) {
		t.Fatalf("fields:\nwant %+v\ngot  %+v", want, cfg.Fields)
	}
	if cfg.TieBreak != "t.id" {
		t.Fatalf("the primary key must be the tie-breaker, got %q", cfg.TieBreak)
	}

	cfg, err = ConfigOf[taggedTemplate](namingDB(), "")
	if err != nil || cfg.Fields[0].Column != "tagged_templates.id" {
		t.Fatalf("an empty table takes the model's own: %v %+v", err, cfg.Fields)
	}
	if _, err := ConfigOf[taggedTemplate](nil, ""); err == nil {
		t.Fatal("want an error without a db")
	}
}
