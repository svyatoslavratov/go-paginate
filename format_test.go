package paginate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func catsConfig() Config {
	cfg := NewConfig("cats", []string{"cats.id", "cats.name", "cats.age"}, map[string]string{
		"id":    "cats.id",
		"name":  "cats.name",
		"age":   "cats.age",
		"color": "cats.color",
	})
	cfg.SearchableColumns = []string{"cats.name", "cats.color"}
	cfg.FilterableColumns = FilterableColumns{"cats.age": {OpGte}, "cats.color": {}}
	cfg.TieBreakColumn = "cats.id"
	cfg.Format = FormatNestJS
	return *cfg
}

// answer runs a query string through the parser and Prepare, then builds the answer for data out
// of total rows. Spaces in query are sent as "+".
func answer(t *testing.T, query string, cfg Config, data []string, total uint64) *Response[string] {
	t.Helper()
	req, err := ParseRequest(httptest.NewRequest(http.MethodGet, "/cats?"+strings.ReplaceAll(query, " ", "+"), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(req, cfg); err != nil {
		t.Fatal(err)
	}
	return NewResponse(data, *req, total)
}

func TestNestJSFormatJSON(t *testing.T) {
	t.Parallel()
	page := answer(t, "page=2&limit=2&search=Pl uto&filter.age=$gte:3&filter.color=white&filter.color=black", catsConfig(), []string{"a", "b"}, 7)
	encoded := encodeJSON(t, page)
	const query = "&limit=2&sortBy=id:ASC&search=Pl+uto&filter.age=$gte:3&filter.color=white&filter.color=black"
	want := `{"data":["a","b"],"meta":{"itemsPerPage":2,"totalItems":7,"currentPage":2,"totalPages":4,` +
		`"sortBy":[["id","ASC"]],"search":"Pl uto","searchBy":["name","color"],"filter":{"age":"$gte:3","color":["white","black"]}},` +
		`"links":{"first":"/cats?page=1` + query + `","previous":"/cats?page=1` + query + `","current":"/cats?page=2` + query +
		`","next":"/cats?page=3` + query + `","last":"/cats?page=4` + query + `"}}`
	if encoded != want {
		t.Fatalf("want %s\ngot  %s", want, encoded)
	}
}

// encodeJSON encodes without HTML escaping, as handlers usually should, so "&" stays readable.
func encodeJSON(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func TestNestJSMetaAndLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		query     string
		config    func(cfg *Config)
		data      []string
		total     uint64
		wantMeta  NestJSMeta
		wantLinks Links
	}{
		{
			name:     "first page: no first and previous",
			query:    "limit=2&sortBy=name:desc&sortBy=age:asc",
			data:     []string{"a", "b"},
			total:    3,
			wantMeta: NestJSMeta{ItemsPerPage: 2, TotalItems: 3, CurrentPage: 1, TotalPages: 2, SortBy: [][2]string{{"name", "DESC"}, {"age", "ASC"}}},
			wantLinks: Links{
				Current: "/cats?page=1&limit=2&sortBy=name:DESC&sortBy=age:ASC",
				Next:    "/cats?page=2&limit=2&sortBy=name:DESC&sortBy=age:ASC",
				Last:    "/cats?page=2&limit=2&sortBy=name:DESC&sortBy=age:ASC",
			},
		},
		{
			name:      "nothing found: only current",
			query:     "search=x",
			total:     0,
			wantMeta:  NestJSMeta{ItemsPerPage: 20, TotalItems: 0, CurrentPage: 1, TotalPages: 0, SortBy: [][2]string{{"id", "ASC"}}, Search: "x", SearchBy: []string{"name", "color"}},
			wantLinks: Links{Current: "/cats?page=1&limit=20&sortBy=id:ASC&search=x"},
		},
		{
			name:      "limit=0 is the count only",
			query:     "limit=0",
			total:     42,
			wantMeta:  NestJSMeta{ItemsPerPage: 42, TotalItems: 42, CurrentPage: 1, TotalPages: 1, SortBy: [][2]string{{"id", "ASC"}}},
			wantLinks: Links{Current: "/cats?page=1&limit=0&sortBy=id:ASC"},
		},
		{
			name:      "limit=-1 when the config allows every row",
			query:     "limit=-1",
			config:    func(cfg *Config) { cfg.AllowDisabledPagination = true },
			data:      []string{"a", "b", "c"},
			total:     3,
			wantMeta:  NestJSMeta{ItemsPerPage: 3, TotalItems: 3, CurrentPage: 1, TotalPages: 1, SortBy: [][2]string{{"id", "ASC"}}},
			wantLinks: Links{Current: "/cats?page=1&limit=-1&sortBy=id:ASC"},
		},
		{
			name:  "base URL, searchBy, expression and withDeleted",
			query: "page=2&limit=1&search=a&searchBy=name&filter=(age=$gte:3 OR color=white)&withDeleted=true",
			config: func(cfg *Config) {
				cfg.LinksBaseURL = "https://api.example.com/v1/"
				cfg.AllowWithDeletedInQuery = true
			},
			data:     []string{"a"},
			total:    2,
			wantMeta: NestJSMeta{ItemsPerPage: 1, TotalItems: 2, CurrentPage: 2, TotalPages: 2, SortBy: [][2]string{{"id", "ASC"}}, Search: "a", SearchBy: []string{"name"}},
			wantLinks: Links{
				First:    "https://api.example.com/v1/cats?page=1&limit=1&sortBy=id:ASC&search=a&searchBy=name&filter=(age%3D$gte:3+OR+color%3Dwhite)&withDeleted=true",
				Previous: "https://api.example.com/v1/cats?page=1&limit=1&sortBy=id:ASC&search=a&searchBy=name&filter=(age%3D$gte:3+OR+color%3Dwhite)&withDeleted=true",
				Current:  "https://api.example.com/v1/cats?page=2&limit=1&sortBy=id:ASC&search=a&searchBy=name&filter=(age%3D$gte:3+OR+color%3Dwhite)&withDeleted=true",
			},
		},
		{
			name:  "default sort by a column without an API field: links leave sortBy out",
			query: "limit=5",
			config: func(cfg *Config) {
				cfg.SortableColumns = append(cfg.SortableColumns, "cats.created_at")
				cfg.DefaultSortBy = SortBy{"cats.created_at": Desc}
			},
			total:     3,
			wantMeta:  NestJSMeta{ItemsPerPage: 5, TotalItems: 3, CurrentPage: 1, TotalPages: 1, SortBy: [][2]string{{"cats.created_at", "DESC"}}},
			wantLinks: Links{Current: "/cats?page=1&limit=5"},
		},
		{
			name:      "page past the end is clamped",
			query:     "page=9&limit=2",
			total:     3,
			wantMeta:  NestJSMeta{ItemsPerPage: 2, TotalItems: 3, CurrentPage: 2, TotalPages: 2, SortBy: [][2]string{{"id", "ASC"}}},
			wantLinks: Links{First: "/cats?page=1&limit=2&sortBy=id:ASC", Previous: "/cats?page=1&limit=2&sortBy=id:ASC", Current: "/cats?page=2&limit=2&sortBy=id:ASC"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := catsConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			page := answer(t, tc.query, cfg, tc.data, tc.total)
			meta, ok := page.Meta.NestJS()
			if !ok {
				t.Fatal("want a nestjs view")
			}
			if !reflect.DeepEqual(meta, tc.wantMeta) {
				t.Errorf("meta:\nwant %+v\ngot  %+v", tc.wantMeta, meta)
			}
			if links := page.Meta.Links(); links != tc.wantLinks {
				t.Errorf("links:\nwant %+v\ngot  %+v", tc.wantLinks, links)
			}
		})
	}
}

func TestDefaultFormatKeepsItsShape(t *testing.T) {
	t.Parallel()
	cfg := catsConfig()
	cfg.Format = FormatDefault
	page := answer(t, "limit=2&sortBy=name:asc", cfg, []string{"a"}, 1)
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":["a"],"meta":{"itemsPerPage":2,"totalItems":1,"currentPage":1,"totalPages":1,"sortBy":{"name":"asc"},"search":"","searchBy":[],"filter":{},"paginationMode":"default"}}`
	if string(encoded) != want {
		t.Fatalf("want %s\ngot  %s", want, encoded)
	}
	// The nestjs view and the links are still there for handlers that map the answer themselves.
	if _, ok := page.Meta.NestJS(); !ok || page.Meta.Links().Current == "" {
		t.Fatal("want the nestjs view and links in the default format too")
	}
}

func TestNestJSFormatSurvivesMapping(t *testing.T) {
	t.Parallel()
	page := answer(t, "limit=2", catsConfig(), []string{"a"}, 1)
	mapped := Response[int]{Data: []int{1}, Meta: page.Meta}
	encoded := encodeJSON(t, mapped)
	want := `{"data":[1],"meta":{"itemsPerPage":2,"totalItems":1,"currentPage":1,"totalPages":1,"sortBy":[["id","ASC"]]},"links":{"current":"/cats?page=1&limit=2&sortBy=id:ASC"}}`
	if encoded != want {
		t.Fatalf("want %s\ngot  %s", want, encoded)
	}
}

func TestHandBuiltMetaHasNoNestJSView(t *testing.T) {
	t.Parallel()
	meta := NewResponse([]string{"a"}, Request{Limit: 10}, 1).Meta
	if _, ok := meta.NestJS(); ok {
		t.Fatal("a request that skipped validation has no nestjs view")
	}
	if meta.Links() != (Links{}) {
		t.Fatal("want no links")
	}
}

func TestEscapeLinkValue(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"$in:1,2":            "$in:1,2",
		"a b&c=d+e":          "a+b%26c%3Dd%2Be",
		"(x OR y)":           "(x+OR+y)",
		"100%":               "100%25",
		"%24 stays escaped":  "%2524+stays+escaped",
		"кот":                "%D0%BA%D0%BE%D1%82",
		`"quoted" and #hash`: "%22quoted%22+and+%23hash",
	}
	for value, want := range tests {
		if got := escapeLinkValue(value); got != want {
			t.Errorf("%q: want %s, got %s", value, want, got)
		}
	}
}
