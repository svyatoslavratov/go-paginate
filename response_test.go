package paginate

import (
	"encoding/json"
	"testing"
)

func TestNewResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		req              Request
		total            uint64
		wantPage         uint64
		wantPages        uint64
		wantItemsPerPage uint64
	}{
		{name: "pages round up", req: Request{Limit: 10, Page: 2}, total: 21, wantPage: 2, wantPages: 3, wantItemsPerPage: 10},
		{name: "page past the end is clamped", req: Request{Limit: 10, Page: 9}, total: 21, wantPage: 3, wantPages: 3, wantItemsPerPage: 10},
		{name: "no rows: page 1 of 0", req: Request{Limit: 10, Page: 4}, total: 0, wantPage: 1, wantPages: 0, wantItemsPerPage: 10},
		{name: "disabled pagination is one page", req: Request{Limit: 10, Mode: ModeDisabled}, total: 35, wantPage: 1, wantPages: 1, wantItemsPerPage: 35},
		{name: "no limit does not divide by zero", req: Request{}, total: 7, wantPage: 1, wantPages: 1, wantItemsPerPage: 7},
		{name: "count mode keeps the page math", req: Request{Limit: 10, Mode: ModeCount}, total: 21, wantPage: 1, wantPages: 3, wantItemsPerPage: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := NewResponse[int](nil, tc.req, tc.total).Meta
			if meta.CurrentPage != tc.wantPage || meta.TotalPages != tc.wantPages || meta.ItemsPerPage != tc.wantItemsPerPage || meta.TotalItems != tc.total {
				t.Fatalf("want page %d of %d by %d, got %+v", tc.wantPage, tc.wantPages, tc.wantItemsPerPage, meta)
			}
		})
	}
}

func TestNewResponseJSON(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(NewResponse[int](nil, Request{Limit: 10}, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":[],"meta":{"itemsPerPage":10,"totalItems":0,"currentPage":1,"totalPages":0,"sortBy":{},"search":"","searchBy":[],"filter":{},"paginationMode":"default"}}`
	if string(encoded) != want {
		t.Fatalf("want %s\ngot  %s", want, encoded)
	}
}
