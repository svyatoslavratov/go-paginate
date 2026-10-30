package paginate

import (
	"net/url"
	"reflect"
	"testing"
)

func catalogConfig() Config {
	return Config{
		TableName:       "templates",
		SortableColumns: []string{"templates.created_at", "templates.order", "templates.score", "templates.pinned", "templates.pin_order"},
		FieldColumns: map[string]string{
			"createdAt": "templates.created_at",
			"order":     "templates.order",
			"score":     "templates.score",
			"pinned":    "templates.pinned",
			"pinOrder":  "templates.pin_order",
		},
		DefaultSortBy: SortBy{"templates.order": Asc},
	}
}

func TestSortEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		keys    []string
		sortBy  map[string]Order
		config  func() Config
		want    []SortEntry
		wantErr bool
	}{
		{
			name:   "request key order is the ORDER BY order",
			keys:   []string{"pinned", "pinOrder", "score", "order"},
			sortBy: map[string]Order{"templates.pinned": Desc, "templates.pin_order": Asc, "templates.score": Desc, "templates.order": Asc},
			config: catalogConfig,
			want:   []SortEntry{{"templates.pinned", Desc}, {"templates.pin_order", Asc}, {"templates.score", Desc}, {"templates.order", Asc}},
		},
		{
			name:   "keys without known order follow SortableColumns order",
			sortBy: map[string]Order{"templates.score": Desc, "templates.pinned": Desc, "templates.order": Asc},
			config: catalogConfig,
			want:   []SortEntry{{"templates.order", Asc}, {"templates.score", Desc}, {"templates.pinned", Desc}},
		},
		{
			name:   "known keys first, the rest in config order",
			keys:   []string{"score"},
			sortBy: map[string]Order{"templates.score": Desc, "templates.pinned": Desc, "templates.order": Asc},
			config: catalogConfig,
			want:   []SortEntry{{"templates.score", Desc}, {"templates.order", Asc}, {"templates.pinned", Desc}},
		},
		{
			name:   "unsortable and unknown keys are dropped",
			keys:   []string{"name", "score", "ghost"},
			sortBy: map[string]Order{"templates.name": Asc, "templates.score": Desc},
			config: catalogConfig,
			want:   []SortEntry{{"templates.score", Desc}},
		},
		{
			name: "no request keys: DefaultSortKeys order",
			config: func() Config {
				cfg := catalogConfig()
				cfg.DefaultSortBy = SortBy{"templates.order": Asc, "templates.score": Desc}
				cfg.DefaultSortKeys = []string{"templates.score", "templates.order"}
				return cfg
			},
			want: []SortEntry{{"templates.score", Desc}, {"templates.order", Asc}},
		},
		{
			name: "default map without keys: alphabetical, deterministic",
			config: func() Config {
				cfg := catalogConfig()
				cfg.DefaultSortBy = SortBy{"templates.score": Desc, "templates.order": Asc}
				return cfg
			},
			want: []SortEntry{{"templates.order", Asc}, {"templates.score", Desc}},
		},
		{
			name: "no defaults: first sortable column ascending",
			config: func() Config {
				cfg := catalogConfig()
				cfg.DefaultSortBy = nil
				return cfg
			},
			want: []SortEntry{{"templates.created_at", Asc}},
		},
		{
			name:    "no sortable columns is a config error",
			config:  func() Config { return Config{TableName: "t"} },
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := tc.config()
			// The order must be the same on every call, whatever the map iteration order.
			for i := 0; i < 50; i++ {
				got, err := sortEntries(&Request{SortKeys: tc.keys}, tc.sortBy, &cfg)
				if tc.wantErr {
					if err == nil {
						t.Fatal("want error")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("run %d: want %v, got %v", i, tc.want, got)
				}
			}
		})
	}
}

func TestOrderBy(t *testing.T) {
	t.Parallel()
	entries := []SortEntry{{"t.pinned", Desc}, {"t.score", Desc}}
	tests := []struct {
		name         string
		query        Query
		withTieBreak bool
		want         []string
	}{
		{name: "entry order", query: Query{Sort: entries}, want: []string{"t.pinned desc", "t.score desc"}},
		{name: "tie-breaker last", query: Query{Sort: entries, TieBreak: "t.id"}, withTieBreak: true, want: []string{"t.pinned desc", "t.score desc", "t.id asc"}},
		{name: "tie-breaker already a key", query: Query{Sort: []SortEntry{{"t.id", Desc}}, TieBreak: "t.id"}, withTieBreak: true, want: []string{"t.id desc"}},
		{name: "tie-breaker left out", query: Query{Sort: entries, TieBreak: "t.id"}, want: []string{"t.pinned desc", "t.score desc"}},
		{name: "nulls last on every key", query: Query{Sort: entries, nullSort: NullsLast}, want: []string{"t.pinned desc NULLS LAST", "t.score desc NULLS LAST"}},
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

func TestParseSortByKeepsOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		query    string
		wantKeys []string
		wantMap  SortBy
		wantErr  bool
	}{
		{
			name:     "order of parameters",
			query:    "sortBy=pinned:desc&sortBy=pinOrder:asc&sortBy=score:DESC&sortBy=order:asc",
			wantKeys: []string{"pinned", "pinOrder", "score", "order"},
			wantMap:  SortBy{"pinned": Desc, "pinOrder": Asc, "score": Desc, "order": Asc},
		},
		{
			name:     "repeated key keeps first position, last order",
			query:    "sortBy=score:desc&sortBy=order:asc&sortBy=score:asc",
			wantKeys: []string{"score", "order"},
			wantMap:  SortBy{"score": Asc, "order": Asc},
		},
		{name: "no sortBy", query: "limit=10", wantKeys: []string{}, wantMap: SortBy{}},
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
			gotMap, gotKeys, err := parseSortBy(values["sortBy"])
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(gotKeys, tc.wantKeys) || !reflect.DeepEqual(gotMap, tc.wantMap) {
				t.Fatalf("want %v %v, got %v %v", tc.wantKeys, tc.wantMap, gotKeys, gotMap)
			}
		})
	}
}
