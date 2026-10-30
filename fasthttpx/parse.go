// Package fasthttpx parses paginate requests from fasthttp.
package fasthttpx

import (
	"net/url"

	"github.com/valyala/fasthttp"

	"github.com/svyatoslavratov/go-paginate"
)

// ParseRequest reads the query string of a fasthttp request into a paginate.Request, the same way
// paginate.ParseRequest does for net/http.
func ParseRequest(ctx *fasthttp.RequestCtx) (*paginate.Request, error) {
	args := ctx.QueryArgs()
	values := make(url.Values, args.Len())
	for key, value := range args.All() {
		name := string(key)
		values[name] = append(values[name], string(value))
	}
	path := (&url.URL{Path: string(ctx.Path())}).EscapedPath()
	return paginate.ParseQuery(values, path)
}
