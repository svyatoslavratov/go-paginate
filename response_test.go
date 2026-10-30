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
	return Config{
		Fields: []Field{
			{Name: "id", Column: "cats.id", Type: Int, Sortable: true},
			{Name: "name", Column: "cats.name", Sortable: true, Searchable: true},
			{Name: "age", Column: "cats.age", Type: Int, Sortable: true, Filter: []Operator{OpGte}},
			{Name: "color", Column: "cats.color", Searchable: true, Filter: []Operator{OpEq}},
		},
		TieBreak: "cats.id",
		Format:   FormatNestJS,
	}
}

// answer runs a query string through the parser and Prepare, then builds the answer for rows and
// total. Spaces in query are sent as "+".
func answer[T any](t *testing.T, query string, cfg Config, rows []T, total Total) *Response[T] {
	t.Helper()
	req, err := ParseRequest(httptest.NewRequest(http.MethodGet, "/cats?"+strings.ReplaceAll(query, " ", "+"), nil))
	if err != nil {
		t.Fatal(err)
	}
	q, err := MustCompile(cfg).Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewResponse(q, rows, total)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func exact(n uint64) Total {
	return Total{Items: n, Count: CountExact}
}

func ptr(n uint64) *uint64 {
	return &n
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

func TestNestJSFormatJSON(t *testing.T) {
	t.Parallel()
	page := answer(t, "page=2&limit=2&search=Pl uto&filter.age=$gte:3&filter.color=white&filter.color=black", catsConfig(), []string{"a", "b"}, exact(7))
	const query = "&limit=2&sortBy=id:ASC&search=Pl+uto&filter.age=$gte:3&filter.color=white&filter.color=black"
	want := `{"data":["a","b"],"meta":{"itemsPerPage":2,"totalItems":7,"currentPage":2,"totalPages":4,` +
		`"sortBy":[["id","ASC"]],"search":"Pl uto","searchBy":["name","color"],"filter":{"age":"$gte:3","color":["white","black"]}},` +
		`"links":{"first":"/cats?page=1` + query + `","previous":"/cats?page=1` + query + `","current":"/cats?page=2` + query +
		`","next":"/cats?page=3` + query + `","last":"/cats?page=4` + query + `"}}`
	if got := encodeJSON(t, page); got != want {
		t.Fatalf("want %s\ngot  %s", want, got)
	}
}

func TestNestJSMetaAndLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		query     string
		config    func(cfg *Config)
		rows      []string
		total     Total
		wantMeta  NestJSMeta
		wantLinks Links
	}{
		{
			name:     "first page: no first and previous",
			query:    "limit=2&sortBy=name:desc&sortBy=age:asc",
			rows:     []string{"a", "b"},
			total:    exact(3),
			wantMeta: NestJSMeta{ItemsPerPage: 2, TotalItems: ptr(3), CurrentPage: ptr(1), TotalPages: ptr(2), SortBy: []Sort{{"name", Desc}, {"age", Asc}}},
			wantLinks: Links{
				Current: "/cats?page=1&limit=2&sortBy=name:DESC&sortBy=age:ASC",
				Next:    "/cats?page=2&limit=2&sortBy=name:DESC&sortBy=age:ASC",
				Last:    "/cats?page=2&limit=2&sortBy=name:DESC&sortBy=age:ASC",
			},
		},
		{
			name:      "nothing found: only current",
			query:     "search=x",
			total:     exact(0),
			wantMeta:  NestJSMeta{ItemsPerPage: 20, TotalItems: ptr(0), CurrentPage: ptr(1), TotalPages: ptr(0), SortBy: []Sort{{"id", Asc}}, Search: "x", SearchBy: []string{"name", "color"}},
			wantLinks: Links{Current: "/cats?page=1&limit=20&sortBy=id:ASC&search=x"},
		},
		{
			name:      "limit=0 is the count only",
			query:     "limit=0",
			total:     exact(42),
			wantMeta:  NestJSMeta{ItemsPerPage: 42, TotalItems: ptr(42), CurrentPage: ptr(1), TotalPages: ptr(1), SortBy: []Sort{{"id", Asc}}},
			wantLinks: Links{Current: "/cats?page=1&limit=0&sortBy=id:ASC"},
		},
		{
			name:      "limit=-1 when the config allows every row: the rows are the count",
			query:     "limit=-1",
			config:    func(cfg *Config) { cfg.AllowDisabledPagination = true },
			rows:      []string{"a", "b", "c"},
			wantMeta:  NestJSMeta{ItemsPerPage: 3, TotalItems: ptr(3), CurrentPage: ptr(1), TotalPages: ptr(1), SortBy: []Sort{{"id", Asc}}},
			wantLinks: Links{Current: "/cats?page=1&limit=-1&sortBy=id:ASC"},
		},
		{
			name:  "base URL, searchBy, expression and withDeleted",
			query: "page=2&limit=1&search=a&searchBy=name&filter=(age=$gte:3 OR color=white)&withDeleted=true",
			config: func(cfg *Config) {
				cfg.LinksBaseURL = "https://api.example.com/v1/"
				cfg.AllowWithDeletedInQuery = true
			},
			rows:     []string{"a"},
			total:    exact(2),
			wantMeta: NestJSMeta{ItemsPerPage: 1, TotalItems: ptr(2), CurrentPage: ptr(2), TotalPages: ptr(2), SortBy: []Sort{{"id", Asc}}, Search: "a", SearchBy: []string{"name"}},
			wantLinks: Links{
				First:    "https://api.example.com/v1/cats?page=1&limit=1&sortBy=id:ASC&search=a&searchBy=name&filter=(age%3D$gte:3+OR+color%3Dwhite)&withDeleted=true",
				Previous: "https://api.example.com/v1/cats?page=1&limit=1&sortBy=id:ASC&search=a&searchBy=name&filter=(age%3D$gte:3+OR+color%3Dwhite)&withDeleted=true",
				Current:  "https://api.example.com/v1/cats?page=2&limit=1&sortBy=id:ASC&search=a&searchBy=name&filter=(age%3D$gte:3+OR+color%3Dwhite)&withDeleted=true",
			},
		},
		{
			name:      "page past the end is clamped",
			query:     "page=9&limit=2",
			total:     exact(3),
			wantMeta:  NestJSMeta{ItemsPerPage: 2, TotalItems: ptr(3), CurrentPage: ptr(2), TotalPages: ptr(2), SortBy: []Sort{{"id", Asc}}},
			wantLinks: Links{First: "/cats?page=1&limit=2&sortBy=id:ASC", Previous: "/cats?page=1&limit=2&sortBy=id:ASC", Current: "/cats?page=2&limit=2&sortBy=id:ASC"},
		},
		{
			name:      "an estimate: totals but no last link",
			query:     "limit=2",
			config:    func(cfg *Config) { cfg.Count = CountEstimate },
			rows:      []string{"a", "b"},
			total:     Total{Items: 5000, Count: CountEstimate},
			wantMeta:  NestJSMeta{ItemsPerPage: 2, TotalItems: ptr(5000), CurrentPage: ptr(1), TotalPages: ptr(2500), SortBy: []Sort{{"id", Asc}}},
			wantLinks: Links{Current: "/cats?page=1&limit=2&sortBy=id:ASC", Next: "/cats?page=2&limit=2&sortBy=id:ASC"},
		},
		{
			name:      "no count: the extra row tells the next page",
			query:     "page=2&limit=2",
			config:    func(cfg *Config) { cfg.Count = CountNone },
			rows:      []string{"a", "b", "c"},
			wantMeta:  NestJSMeta{ItemsPerPage: 2, CurrentPage: ptr(2), SortBy: []Sort{{"id", Asc}}},
			wantLinks: Links{First: "/cats?page=1&limit=2&sortBy=id:ASC", Previous: "/cats?page=1&limit=2&sortBy=id:ASC", Current: "/cats?page=2&limit=2&sortBy=id:ASC", Next: "/cats?page=3&limit=2&sortBy=id:ASC"},
		},
		{
			name:      "no count, no extra row: the last page",
			query:     "limit=2",
			config:    func(cfg *Config) { cfg.Count = CountNone },
			rows:      []string{"a"},
			wantMeta:  NestJSMeta{ItemsPerPage: 2, CurrentPage: ptr(1), SortBy: []Sort{{"id", Asc}}},
			wantLinks: Links{Current: "/cats?page=1&limit=2&sortBy=id:ASC"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := catsConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			page := answer(t, tc.query, cfg, tc.rows, tc.total)
			meta, ok := page.Meta.NestJS()
			if !ok {
				t.Fatal("want a nestjs view")
			}
			if !reflect.DeepEqual(meta, tc.wantMeta) {
				t.Errorf("meta:\nwant %s\ngot  %s", encodeJSON(t, tc.wantMeta), encodeJSON(t, meta))
			}
			if links := page.Meta.Links(); links != tc.wantLinks {
				t.Errorf("links:\nwant %+v\ngot  %+v", tc.wantLinks, links)
			}
		})
	}
}

func TestPageMath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		query            string
		config           func(cfg *Config)
		rows             []int
		total            Total
		wantPage         uint64
		wantPages        uint64
		wantItems        uint64
		wantItemsPerPage uint64
		wantNext         bool
		wantPrevious     bool
		wantRows         int
	}{
		{name: "pages round up", query: "limit=10&page=2", total: exact(21), wantPage: 2, wantPages: 3, wantItems: 21, wantItemsPerPage: 10, wantNext: true, wantPrevious: true},
		{name: "page past the end is clamped", query: "limit=10&page=9", total: exact(21), wantPage: 3, wantPages: 3, wantItems: 21, wantItemsPerPage: 10, wantPrevious: true},
		{name: "no rows: page 1 of 0", query: "limit=10&page=4", total: exact(0), wantPage: 1, wantItemsPerPage: 10},
		{
			name:   "every row is one page",
			query:  "limit=-1",
			config: func(cfg *Config) { cfg.AllowDisabledPagination = true },
			rows:   make([]int, 35), wantPage: 1, wantPages: 1, wantItems: 35, wantItemsPerPage: 35, wantRows: 35,
		},
		{name: "the count only is one page of every item", query: "limit=0", total: exact(21), wantPage: 1, wantPages: 1, wantItems: 21, wantItemsPerPage: 21},
		{
			name:     "without a count the extra row is dropped",
			query:    "limit=2",
			config:   func(cfg *Config) { cfg.Count = CountNone },
			rows:     []int{1, 2, 3},
			wantPage: 1, wantItemsPerPage: 2, wantNext: true, wantRows: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := catsConfig()
			cfg.Format = FormatDefault
			if tc.config != nil {
				tc.config(&cfg)
			}
			page := answer(t, tc.query, cfg, tc.rows, tc.total)
			meta := page.Meta
			if meta.CurrentPage != tc.wantPage || meta.TotalPages != tc.wantPages || meta.TotalItems != tc.wantItems ||
				meta.ItemsPerPage != tc.wantItemsPerPage || meta.HasNextPage != tc.wantNext || meta.HasPreviousPage != tc.wantPrevious ||
				len(page.Data) != tc.wantRows {
				t.Fatalf("got %+v with %d rows", meta, len(page.Data))
			}
		})
	}
}

func TestDefaultFormatJSON(t *testing.T) {
	t.Parallel()
	cfg := catsConfig()
	cfg.Format = FormatDefault
	page := answer(t, "limit=2&sortBy=name:asc&sortBy=age:desc", cfg, []string{"a"}, exact(1))
	want := `{"data":["a"],"meta":{"itemsPerPage":2,"totalItems":1,"totalPages":1,"currentPage":1,"hasNextPage":false,"hasPreviousPage":false,` +
		`"sortBy":[["name","ASC"],["age","DESC"]],"search":"","searchBy":[],"filter":{},"paginationMode":"default"}}`
	if got := encodeJSON(t, page); got != want {
		t.Fatalf("want %s\ngot  %s", want, got)
	}
	// The nestjs view and the links are there for handlers that map the answer themselves.
	if _, ok := page.Meta.NestJS(); !ok || page.Meta.Links().Current == "" {
		t.Fatal("want the nestjs view and links in the default format too")
	}

	cfg.Count = CountEstimate
	page = answer(t, "limit=2", cfg, []string{"a", "b"}, Total{Items: 1000, Count: CountEstimate})
	if got := encodeJSON(t, page.Meta); !strings.Contains(got, `"totalItems":1000,"totalPages":500,"estimated":true,`) {
		t.Fatalf("an estimate must be marked: %s", got)
	}
	cfg.Count = CountNone
	page = answer(t, "limit=2", cfg, []string{"a", "b", "c"}, Total{})
	if got := encodeJSON(t, page.Meta); strings.Contains(got, "totalItems") || !strings.Contains(got, `"hasNextPage":true`) {
		t.Fatalf("without a count there are no totals: %s", got)
	}
}

func TestHandBuiltMeta(t *testing.T) {
	t.Parallel()
	meta := Meta{ItemsPerPage: 10, TotalItems: 1, TotalPages: 1, CurrentPage: 1}
	if _, ok := meta.NestJS(); ok {
		t.Fatal("a meta built by hand has no nestjs view")
	}
	if meta.Links() != (Links{}) {
		t.Fatal("want no links")
	}
	want := `{"itemsPerPage":10,"totalItems":1,"totalPages":1,"currentPage":1,"hasNextPage":false,"hasPreviousPage":false,"sortBy":[],"search":"","searchBy":[],"filter":{},"paginationMode":"default"}`
	if got := encodeJSON(t, meta); got != want {
		t.Fatalf("want %s\ngot  %s", want, got)
	}
}

func TestMap(t *testing.T) {
	t.Parallel()
	page := answer(t, "limit=2", catsConfig(), []string{"a"}, exact(1))
	mapped := Map(page, strings.ToUpper)
	want := `{"data":["A"],"meta":{"itemsPerPage":2,"totalItems":1,"currentPage":1,"totalPages":1,"sortBy":[["id","ASC"]]},"links":{"current":"/cats?page=1&limit=2&sortBy=id:ASC"}}`
	if got := encodeJSON(t, mapped); got != want {
		t.Fatalf("Map must keep the meta and the links:\nwant %s\ngot  %s", want, got)
	}
}

func TestEstimateFromPlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		plan    string
		want    uint64
		wantErr bool
	}{
		{plan: `[{"Plan": {"Node Type": "Seq Scan", "Plan Rows": 1234}}]`, want: 1234},
		{plan: `[{"Plan": {"Plan Rows": 2.6}}]`, want: 3},
		{plan: `[{"Plan": {"Plan Rows": 0}}]`, want: 0},
		{plan: `[]`, wantErr: true},
		{plan: `[{"Plan": {"Node Type": "Result"}}]`, wantErr: true},
		{plan: `[{"Plan": {"Plan Rows": -1}}]`, wantErr: true},
		{plan: `not json`, wantErr: true},
	}
	for _, tc := range tests {
		got, err := EstimateFromPlan([]byte(tc.plan))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: want %d (error %v), got %d, %v", tc.plan, tc.want, tc.wantErr, got, err)
		}
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

func TestLinksStayOnTheHost(t *testing.T) {
	t.Parallel()
	req, err := ParseRequest(httptest.NewRequest(http.MethodGet, "http://api.example.com//evil.example/cats?limit=2", nil))
	if err != nil {
		t.Fatal(err)
	}
	q, err := MustCompile(catsConfig()).Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewResponse(q, []string{"a", "b"}, exact(5))
	if err != nil {
		t.Fatal(err)
	}
	if links := page.Meta.Links(); !strings.HasPrefix(links.Current, "/evil.example/cats?") || !strings.HasPrefix(links.Next, "/evil.example/cats?") {
		t.Fatalf("a link must not lead to another host: %+v", links)
	}
}
