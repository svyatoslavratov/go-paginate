package paginate

import (
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"testing"
)

func catalogConfig() Config {
	return Config{
		Fields: []Field{
			{Name: "createdAt", Column: "templates.created_at", Sortable: true},
			{Name: "order", Column: "templates.order", Sortable: true},
			{Name: "score", Column: "templates.score", Sortable: true},
			{Name: "pinned", Column: "templates.pinned", Sortable: true},
			{Name: "pinOrder", Column: "templates.pin_order", Sortable: true},
			{Name: "name", Column: "templates.name"},
		},
		DefaultSort: []Sort{{"order", Asc}},
	}
}

func TestSortKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sortBy  []Sort
		config  func(cfg *Config)
		want    []SortEntry
		wantErr Code
	}{
		{
			name:   "request order is the ORDER BY order",
			sortBy: []Sort{{"pinned", Desc}, {"pinOrder", Asc}, {"score", Desc}, {"order", Asc}},
			want:   []SortEntry{{"templates.pinned", Desc}, {"templates.pin_order", Asc}, {"templates.score", Desc}, {"templates.order", Asc}},
		},
		{
			name:   "a repeated field keeps its first place",
			sortBy: []Sort{{"score", Desc}, {"order", Asc}, {"score", Asc}},
			want:   []SortEntry{{"templates.score", Desc}, {"templates.order", Asc}},
		},
		{name: "a field that is not sortable fails", sortBy: []Sort{{"name", Asc}}, wantErr: CodeSortNotAllowed},
		{name: "an unknown field fails", sortBy: []Sort{{"ghost", Asc}}, wantErr: CodeSortNotAllowed},
		{
			name:   "Lenient drops what is not sortable",
			sortBy: []Sort{{"name", Asc}, {"score", Desc}, {"ghost", Asc}},
			config: func(cfg *Config) { cfg.Lenient = true },
			want:   []SortEntry{{"templates.score", Desc}},
		},
		{
			name:   "no keys: the default sort in order, any field",
			config: func(cfg *Config) { cfg.DefaultSort = []Sort{{"score", Desc}, {"name", Asc}} },
			want:   []SortEntry{{"templates.score", Desc}, {"templates.name", Asc}},
		},
		{
			name:   "no default: the first sortable field ascending",
			config: func(cfg *Config) { cfg.DefaultSort = nil },
			want:   []SortEntry{{"templates.created_at", Asc}},
		},
		{
			name: "nothing sortable: the tie-breaker alone",
			config: func(cfg *Config) {
				cfg.DefaultSort = nil
				cfg.Fields = []Field{{Name: "name", Column: "templates.name"}}
				cfg.TieBreak = "templates.id"
			},
			want: []SortEntry{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := catalogConfig()
			if tc.config != nil {
				tc.config(&cfg)
			}
			q, err := MustCompile(cfg).Prepare(&Request{SortBy: tc.sortBy})
			if tc.wantErr != "" {
				wantError(t, err, tc.wantErr, tc.sortBy[0].Field, "sorting by")
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(q.Sort, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, q.Sort)
			}
		})
	}
}

func TestOrderBy(t *testing.T) {
	t.Parallel()
	entries := []SortEntry{{"t.pinned", Desc}, {"t.score", Asc}}
	tests := []struct {
		name         string
		query        Query
		withTieBreak bool
		want         []string
	}{
		{name: "entry order", query: Query{Sort: entries}, want: []string{"t.pinned DESC", "t.score ASC"}},
		{name: "tie-breaker last", query: Query{Sort: entries, TieBreak: "t.id"}, withTieBreak: true, want: []string{"t.pinned DESC", "t.score ASC", "t.id ASC"}},
		{name: "tie-breaker already a key", query: Query{Sort: []SortEntry{{"t.id", Desc}}, TieBreak: "t.id"}, withTieBreak: true, want: []string{"t.id DESC"}},
		{name: "tie-breaker left out", query: Query{Sort: entries, TieBreak: "t.id"}, want: []string{"t.pinned DESC", "t.score ASC"}},
		{name: "nulls last on every key", query: Query{Sort: entries, nullSort: NullsLast}, want: []string{"t.pinned DESC NULLS LAST", "t.score ASC NULLS LAST"}},
		{
			name:         "backward: every key flips with its database NULL placement",
			query:        Query{Sort: entries, TieBreak: "t.id", backward: true},
			withTieBreak: true,
			want:         []string{"t.pinned ASC NULLS LAST", "t.score DESC NULLS FIRST", "t.id DESC NULLS FIRST"},
		},
		{
			name:  "backward with NULLS LAST: NULL values come first",
			query: Query{Sort: entries, nullSort: NullsLast, backward: true},
			want:  []string{"t.pinned ASC NULLS FIRST", "t.score DESC NULLS FIRST"},
		},
		{name: "empty", query: Query{}, want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.query.OrderBy(tc.withTieBreak); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
	// OrderBy must not change the sort it was given.
	q := Query{Sort: entries[:1:1], TieBreak: "t.id"}
	q.OrderBy(true)
	if len(q.Sort) != 1 {
		t.Fatalf("OrderBy changed Sort: %v", q.Sort)
	}
}

func TestParseSortBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		query   string
		want    []Sort
		wantErr bool
	}{
		{
			name:  "order of parameters, any case",
			query: "sortBy=pinned:desc&sortBy=pinOrder:asc&sortBy=score:DESC&sortBy=order:Asc",
			want:  []Sort{{"pinned", Desc}, {"pinOrder", Asc}, {"score", Desc}, {"order", Asc}},
		},
		{
			name:  "a repeated field keeps its first position and its last order",
			query: "sortBy=score:desc&sortBy=order:asc&sortBy=score:asc",
			want:  []Sort{{"score", Asc}, {"order", Asc}},
		},
		{name: "no sortBy", query: "limit=10", want: []Sort{}},
		{name: "no order", query: "sortBy=score", wantErr: true},
		{name: "bad order", query: "sortBy=score:up", wantErr: true},
		{name: "extra part", query: "sortBy=a:asc:desc", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			values, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseSortBy(values["sortBy"])
			if tc.wantErr {
				var pe *Error
				if !errors.As(err, &pe) || pe.Code != CodeInvalidSort {
					t.Fatalf("want an invalid sort, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestSortJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal([]Sort{{"createdAt", Desc}, {"name", Asc}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[["createdAt","DESC"],["name","ASC"]]` {
		t.Fatalf("got %s", data)
	}
	var back []Sort
	if err := json.Unmarshal([]byte(`[["createdAt","desc"],["name","ASC"]]`), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, []Sort{{"createdAt", Desc}, {"name", Asc}}) {
		t.Fatalf("got %v", back)
	}
	if err := json.Unmarshal([]byte(`[["a","sideways"]]`), &back); err == nil {
		t.Fatal("want an error for an unknown order")
	}
}
