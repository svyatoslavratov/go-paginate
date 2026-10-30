package catalog

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestConfigIsValid(t *testing.T) {
	if err := Config().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFieldsCoverTheConfig(t *testing.T) {
	cfg := Config()
	if len(Fields()) != len(cfg.FieldColumns) {
		t.Fatalf("%d fields described, %d mapped", len(Fields()), len(cfg.FieldColumns))
	}
	for _, field := range Fields() {
		if _, ok := cfg.FieldColumns[field.Name]; !ok {
			t.Errorf("field %s is not in the config", field.Name)
		}
	}
}

func TestTagsScan(t *testing.T) {
	var tags Tags
	if err := tags.Scan(`{sale,"new arrival"}`); err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[1] != "new arrival" {
		t.Fatalf("got %q", tags)
	}
}

func TestProductIsAGORMModel(t *testing.T) {
	model, err := schema.Parse(&Product{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for field, column := range map[string]string{"SKU": "sku", "Tags": "tags", "DiscontinuedAt": "discontinued_at"} {
		if got := model.LookUpField(field); got == nil || got.DBName != column {
			t.Errorf("field %s: got %+v, want column %s", field, got, column)
		}
	}
}
