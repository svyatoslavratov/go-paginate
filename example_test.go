package paginate_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"

	"github.com/svyatoslavratov/go-paginate"
)

func usersConfig() paginate.Config {
	cfg := paginate.NewConfig("users", []string{"users.created_at", "users.name"}, map[string]string{
		"createdAt": "users.created_at",
		"name":      "users.name",
		"email":     "users.email",
		"status":    "users.status",
		"age":       "users.age",
	})
	cfg.SearchableColumns = []string{"users.name", "users.email"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"users.status": {paginate.OpIn},
		"users.age":    {paginate.OpGte, paginate.OpLt},
		"users.name":   {},
	}
	cfg.TieBreakColumn = "users.id"
	return *cfg
}

// ParseRequest reads the nestjs-paginate query parameters of a net/http request.
func ExampleParseRequest() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=10&sortBy=name:asc&search=ann&filter.status=$in:active,invited", nil)
	req, err := paginate.ParseRequest(r)
	if err != nil {
		panic(err)
	}
	fmt.Println(req.Page, req.Limit, req.SortBy, req.Search, req.Filter, req.Path)
	// Output:
	// 2 10 map[name:asc] ann map[status:[$in:active,invited]] /users
}

// Prepare checks a request against the config and renders it for any executor.
func ExamplePrepare() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=10&sortBy=name:asc&search=ann&filter.status=$in:active,invited&filter.age=$gte:18&filter.age=$lt:65", nil)
	req, _ := paginate.ParseRequest(r)

	q, err := paginate.Prepare(req, usersConfig())
	if err != nil {
		panic(err)
	}
	for _, condition := range q.Where {
		fmt.Println(condition.SQL, condition.Args)
	}
	fmt.Println(q.OrderBy(true), q.Limit, q.Offset)
	// Output:
	// ((CAST(users.name AS TEXT) ILIKE ?) OR (CAST(users.email AS TEXT) ILIKE ?)) [%ann% %ann%]
	// (((users.age >= ?) AND (users.age < ?)) AND (users.status IN (?, ?))) [18 65 active invited]
	// [users.name asc users.id asc] 10 10
}

// A filter expression combines terms with AND, OR, NOT and parentheses, as in nestjs-paginate 15+.
func ExamplePrepare_filterExpression() {
	query := url.Values{"filter": {`(status=$in:active,invited OR age=$gte:65) AND NOT name=$eq:"Ann Lee"`}}
	r := httptest.NewRequest(http.MethodGet, "/users?"+query.Encode(), nil)
	req, _ := paginate.ParseRequest(r)

	q, err := paginate.Prepare(req, usersConfig())
	if err != nil {
		panic(err)
	}
	fmt.Println(q.Where[0].SQL)
	fmt.Println(q.Where[0].Args...)
	// Output:
	// (((users.status IN (?, ?)) OR (users.age >= ?)) AND (NOT (users.name = ?)))
	// active invited 65 Ann Lee
}

// The meta of an answer echoes the request, so a client can render its controls from it.
func ExampleNewResponse() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=2&sortBy=name:asc&filter.status=active", nil)
	req, _ := paginate.ParseRequest(r)
	if _, err := paginate.Prepare(req, usersConfig()); err != nil {
		panic(err)
	}

	page := paginate.NewResponse([]string{"Ann", "Bob"}, *req, 5)
	encoded, _ := json.MarshalIndent(page, "", "  ")
	fmt.Println(string(encoded))
	// Output:
	// {
	//   "data": [
	//     "Ann",
	//     "Bob"
	//   ],
	//   "meta": {
	//     "itemsPerPage": 2,
	//     "totalItems": 5,
	//     "currentPage": 2,
	//     "totalPages": 3,
	//     "sortBy": {
	//       "name": "asc"
	//     },
	//     "search": "",
	//     "searchBy": [],
	//     "filter": {
	//       "status": [
	//         "active"
	//       ]
	//     },
	//     "paginationMode": "default"
	//   }
	// }
}

// FormatNestJS answers the nestjs-paginate way: the effective sort in meta.sortBy, and links.
func ExampleFormat() {
	cfg := usersConfig()
	cfg.Format = paginate.FormatNestJS

	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=1&filter.age=$gte:18", nil)
	req, _ := paginate.ParseRequest(r)
	if _, err := paginate.Prepare(req, cfg); err != nil {
		panic(err)
	}

	page := paginate.NewResponse([]string{"Ann"}, *req, 3)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(page)
	// Output:
	// {
	//   "data": [
	//     "Ann"
	//   ],
	//   "meta": {
	//     "itemsPerPage": 1,
	//     "totalItems": 3,
	//     "currentPage": 2,
	//     "totalPages": 3,
	//     "sortBy": [
	//       [
	//         "createdAt",
	//         "ASC"
	//       ]
	//     ],
	//     "filter": {
	//       "age": "$gte:18"
	//     }
	//   },
	//   "links": {
	//     "first": "/users?page=1&limit=1&sortBy=createdAt:ASC&filter.age=$gte:18",
	//     "previous": "/users?page=1&limit=1&sortBy=createdAt:ASC&filter.age=$gte:18",
	//     "current": "/users?page=2&limit=1&sortBy=createdAt:ASC&filter.age=$gte:18",
	//     "next": "/users?page=3&limit=1&sortBy=createdAt:ASC&filter.age=$gte:18",
	//     "last": "/users?page=3&limit=1&sortBy=createdAt:ASC&filter.age=$gte:18"
	//   }
	// }
}

// Validate reports config mistakes that requests would only hit one at a time; call it in a test.
func ExampleConfig_Validate() {
	cfg := paginate.NewConfig("reviews", []string{"rating"}, map[string]string{
		"rating": "reviews.published_rating",
	})
	fmt.Println(cfg.Validate())
	// Output:
	// paginate: invalid config: sortable column rating: no API field maps to it
}
