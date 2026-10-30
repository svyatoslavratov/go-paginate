package paginate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type tagBase struct {
	ID        int64     `json:"id" paginate:"unique;sort"`
	CreatedAt time.Time `json:"createdAt" paginate:"sort;filter:$gte,$lt"`
}

type tagAuthor struct {
	Name string `json:"name" paginate:"sort;column:authors.name"`
	Bio  string `json:"bio"`
}

type tagProduct struct {
	tagBase
	Title    string    `json:"title" paginate:"search;filter:*"`
	Price    float64   `json:"price" paginate:"sort;filter:$btw;notnull"`
	Status   string    `json:"status" paginate:"filter:$in;enum:draft, published"`
	Tags     []string  `json:"tags" paginate:"filter:$contains;enum:new,sale"`
	SKU      string    `json:"sku" paginate:"type:uuid;filter" db:"sku_code"`
	Legacy   string    `json:"legacy" paginate:"name:old;column:COALESCE(p.legacy, '')"`
	Shop     int       `json:"shopId" gorm:"column:shop_ref" paginate:"filter"`
	Author   tagAuthor `json:"author"`
	Skipped  string    `json:"skipped" paginate:"-"`
	Untagged string    `json:"untagged"`
	Hidden   string    `json:"-" paginate:"filter"`
}

func TestConfigOf(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigOf[tagProduct]("p")
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{Name: "id", Column: "p.id", Type: Int, Sortable: true},
		{Name: "createdAt", Column: "p.created_at", Type: Time, Sortable: true, Filter: []Operator{OpEq, OpGte, OpLt}},
		{Name: "title", Column: "p.title", Type: Text, Searchable: true, Filter: AllOperators},
		{Name: "price", Column: "p.price", Type: Number, Sortable: true, Filter: []Operator{OpEq, OpBtw}, NotNull: true},
		{Name: "status", Column: "p.status", Type: Enum("draft", "published"), Filter: []Operator{OpEq, OpIn}},
		{Name: "tags", Column: "p.tags", Type: ArrayOf(Enum("new", "sale")), Filter: []Operator{OpEq, OpContains}},
		{Name: "sku", Column: "p.sku_code", Type: UUID, Filter: []Operator{OpEq}},
		{Name: "old", Column: "COALESCE(p.legacy, '')", Type: Text},
		{Name: "shopId", Column: "p.shop_ref", Type: Int, Filter: []Operator{OpEq}},
		{Name: "author.name", Column: "authors.name", Type: Text, Sortable: true},
		{Name: "Hidden", Column: "p.hidden", Type: Text, Filter: []Operator{OpEq}},
	}
	if !reflect.DeepEqual(cfg.Fields, want) {
		for i := range max(len(want), len(cfg.Fields)) {
			if i >= len(want) || i >= len(cfg.Fields) || !reflect.DeepEqual(cfg.Fields[i], want[i]) {
				t.Errorf("field %d:\nwant %+v\ngot  %+v", i, safeField(want, i), safeField(cfg.Fields, i))
			}
		}
	}
	if cfg.TieBreak != "p.id" {
		t.Fatalf("tie-breaker %q", cfg.TieBreak)
	}
	if _, err := Compile(cfg); err != nil {
		t.Fatalf("the config must compile: %v", err)
	}
}

func safeField(fields []Field, i int) any {
	if i < len(fields) {
		return fields[i]
	}
	return nil
}

func TestConfigOfErrors(t *testing.T) {
	t.Parallel()
	type broken struct {
		A string `paginate:"sort;fly"`
		B string `paginate:"type:money"`
		C struct {
			D string `json:"d" paginate:"sort"`
		} `json:"c"`
	}
	_, err := ConfigOf[broken]("t")
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want a config error, got %v", err)
	}
	for _, want := range []string{`broken.A: unknown paginate setting "fly"`, `broken.B: unknown type "money"`, "broken.C.D: a field of a nested struct needs a column setting"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
	if _, err := ConfigOf[int]("t"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("a non-struct must fail, got %v", err)
	}
}
