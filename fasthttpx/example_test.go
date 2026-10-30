package fasthttpx_test

import (
	"fmt"

	"github.com/valyala/fasthttp"

	"github.com/svyatoslavratov/go-paginate/fasthttpx"
)

// ParseRequest reads the same parameters as paginate.ParseRequest does for net/http.
func ExampleParseRequest() {
	var ctx fasthttp.RequestCtx
	ctx.Request.SetRequestURI("/users?page=2&limit=10&sortBy=name:desc&filter.status=$in:active,invited")

	req, err := fasthttpx.ParseRequest(&ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(req.Page, req.Limit, req.SortBy, req.Filter, req.Path)
	// Output:
	// 2 10 map[name:desc] map[status:[$in:active,invited]] /users
}
