package catalog

import (
	"net/url"
	"sync"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/svyatoslavratov/go-paginate"
)

func TestConfigCompiles(t *testing.T) {
	if _, err := paginate.Compile(Config()); err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 12 {
		t.Fatalf("want every combination of options compiled, got %d", len(endpoints))
	}
}

func TestFieldsCoverTheConfig(t *testing.T) {
	fields := Fields()
	if len(fields) != len(Config().Fields) {
		t.Fatalf("%d fields described, %d in the config", len(fields), len(Config().Fields))
	}
	for _, field := range fields {
		if field.Label == "" {
			t.Errorf("field %s has no label", field.Name)
		}
	}
	if fields[4].Name != "category" || len(fields[4].Values) != 5 || fields[8].Type != "tags" || len(fields[8].Values) != 5 {
		t.Fatalf("enums: %+v %+v", fields[4], fields[8])
	}
}

func TestOptionsOf(t *testing.T) {
	got := OptionsOf(url.Values{"format": {"default"}, "pages": {"cursor"}, "count": {"estimate"}})
	want := Options{Format: paginate.FormatDefault, Pagination: paginate.CursorPagination, Count: paginate.CountEstimate}
	if got != want || Endpoint(got) == nil {
		t.Fatalf("want %+v, got %+v", want, got)
	}
	if got := OptionsOf(url.Values{}); got != (Options{Format: paginate.FormatNestJS}) {
		t.Fatalf("defaults: %+v", got)
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
