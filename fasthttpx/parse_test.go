package fasthttpx

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/valyala/fasthttp"

	"github.com/svyatoslavratov/go-paginate"
)

func TestParseRequestMatchesNetHTTP(t *testing.T) {
	t.Parallel()
	queries := []string{
		"page=2&limit=10&sortBy=name:DESC&sortBy=id:asc&search=milo&searchBy=name&searchBy=email&filter.age=$gt:3&filter.age=$or:$null&filter.name=",
		"limit=0",
		"limit=-1",
		"paginationMode=count",
		"filter=a%3D1+OR+b%3D2&filter=c%3D3&withDeleted=true",
		"page=-1&limit=abc",
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			var ctx fasthttp.RequestCtx
			ctx.Request.SetRequestURI("/items?" + query)
			got, err := ParseRequest(&ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			values, err := url.ParseQuery(query)
			if err != nil {
				t.Fatal(err)
			}
			want, err := paginate.ParseQuery(values, "/items")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("want %+v\ngot  %+v", want, got)
			}
		})
	}
}

func TestParseRequestBadSortBy(t *testing.T) {
	t.Parallel()
	var ctx fasthttp.RequestCtx
	ctx.Request.SetRequestURI("/items?sortBy=name:up")
	if _, err := ParseRequest(&ctx); err == nil {
		t.Fatal("want error")
	}
}
