package paginate

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// parsers runs a query string through both HTTP adapters: they must agree.
var parsers = map[string]func(t *testing.T, query string) (*Request, error){
	"net/http": func(_ *testing.T, query string) (*Request, error) {
		return ParseRequest(httptest.NewRequest(http.MethodGet, "/items?"+query, nil))
	},
}

func TestParseQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		check func(t *testing.T, req *Request)
	}{
		{
			name:  "nestjs-paginate parameters",
			query: "page=2&limit=10&sortBy=name:DESC&sortBy=id:asc&search=milo&searchBy=name&searchBy=email&filter.age=$gt:3&filter.age=$or:$null&filter.name=",
			check: func(t *testing.T, req *Request) {
				if req.Page != 2 || req.Limit != 10 {
					t.Fatalf("page/limit: %d/%d", req.Page, req.Limit)
				}
				if !reflect.DeepEqual(req.SortBy, SortBy{"name": Desc, "id": Asc}) || !reflect.DeepEqual(req.SortKeys, []string{"name", "id"}) {
					t.Fatalf("sortBy: %v %v", req.SortBy, req.SortKeys)
				}
				if req.Search != "milo" || !reflect.DeepEqual(req.SearchBy, []string{"name", "email"}) {
					t.Fatalf("search: %q %v", req.Search, req.SearchBy)
				}
				// Values keep request order; empty values are dropped.
				if !reflect.DeepEqual(req.Filter, FilterQuery{"age": {"$gt:3", "$or:$null"}}) {
					t.Fatalf("filter: %v", req.Filter)
				}
				if req.Mode != ModeDefault || req.FilterExpression != "" || req.WithDeleted {
					t.Fatalf("defaults: %+v", req)
				}
			},
		},
		{
			name:  "garbage page and limit fall back",
			query: "page=-1&limit=abc",
			check: func(t *testing.T, req *Request) {
				if req.Page != 0 || req.Limit != 0 {
					t.Fatalf("page/limit: %d/%d", req.Page, req.Limit)
				}
			},
		},
		{
			name:  "paginationMode=disabled waits for the config",
			query: "paginationMode=disabled",
			check: func(t *testing.T, req *Request) {
				if req.RequestedMode != ModeDisabled || req.Mode != "" {
					t.Fatalf("modes: %q/%q", req.RequestedMode, req.Mode)
				}
			},
		},
		{
			name:  "limit=-1 asks for every row",
			query: "limit=-1",
			check: func(t *testing.T, req *Request) {
				if req.RequestedMode != ModeDisabled || req.Mode != "" {
					t.Fatalf("modes: %q/%q", req.RequestedMode, req.Mode)
				}
			},
		},
		{
			name:  "paginationMode=count applies at once",
			query: "paginationMode=count",
			check: func(t *testing.T, req *Request) {
				if req.Mode != ModeCount {
					t.Fatalf("mode: %q", req.Mode)
				}
			},
		},
		{
			name:  "filter expression and withDeleted are kept for validation",
			query: "filter=(name%3D$eq:a%20OR%20name%3D$eq:b)&withDeleted=true",
			check: func(t *testing.T, req *Request) {
				if req.FilterExpression != "(name=$eq:a OR name=$eq:b)" || !req.WithDeleted {
					t.Fatalf("got %q %v", req.FilterExpression, req.WithDeleted)
				}
			},
		},
		{
			name:  "limit=0 asks for the count only",
			query: "limit=0",
			check: func(t *testing.T, req *Request) {
				if req.Mode != ModeCount || req.RequestedMode != ModeCount {
					t.Fatalf("modes: %q/%q", req.RequestedMode, req.Mode)
				}
			},
		},
		{
			name:  "paginationMode wins over limit",
			query: "limit=0&paginationMode=default",
			check: func(t *testing.T, req *Request) {
				if req.Mode != ModeDefault {
					t.Fatalf("mode: %q", req.Mode)
				}
			},
		},
		{
			name:  "the path is kept for links",
			query: "page=2",
			check: func(t *testing.T, req *Request) {
				if req.Path != "/items" {
					t.Fatalf("path: %q", req.Path)
				}
			},
		},
		{
			name:  "repeated filter expressions are ANDed",
			query: "filter=a%3D1+OR+b%3D2&filter=&filter=c%3D3",
			check: func(t *testing.T, req *Request) {
				if req.FilterExpression != "(a=1 OR b=2) AND (c=3)" {
					t.Fatalf("expression: %q", req.FilterExpression)
				}
			},
		},
	}
	for parserName, parse := range parsers {
		for _, tc := range tests {
			t.Run(parserName+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				req, err := parse(t, tc.query)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				tc.check(t, req)
			})
		}
	}
}

func TestParseQueryBadSortBy(t *testing.T) {
	t.Parallel()
	for parserName, parse := range parsers {
		for _, query := range []string{"sortBy=name", "sortBy=name:up", "sortBy=a:b:c"} {
			t.Run(parserName+"/"+query, func(t *testing.T) {
				t.Parallel()
				if _, err := parse(t, query); err == nil {
					t.Fatal("want error")
				}
			})
		}
	}
}
