package paginate_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/svyatoslavratov/go-paginate"
)

func usersConfig() paginate.Config {
	return paginate.Config{
		Fields: []paginate.Field{
			{Name: "id", Column: "users.id", Type: paginate.Int},
			{Name: "createdAt", Column: "users.created_at", Type: paginate.Time, Sortable: true},
			{Name: "name", Column: "users.name", Sortable: true, Searchable: true, Filter: []paginate.Operator{paginate.OpEq}},
			{Name: "email", Column: "users.email", Searchable: true},
			{Name: "status", Column: "users.status", Type: paginate.Enum("active", "invited", "blocked"), Filter: []paginate.Operator{paginate.OpIn}},
			{Name: "age", Column: "users.age", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpGte, paginate.OpLt}},
		},
		TieBreak: "users.id",
	}
}

var usersEndpoint = paginate.MustCompile(usersConfig())

// ParseRequest reads the nestjs-paginate query parameters of a net/http request.
func ExampleParseRequest() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=10&sortBy=name:asc&search=ann&filter.status=$in:active,invited", nil)
	req, err := paginate.ParseRequest(r)
	if err != nil {
		panic(err)
	}
	fmt.Println(req.Page, req.Limit, req.SortBy, req.Search, req.Filter, req.Path)
	// Output:
	// 2 10 [{name ASC}] ann map[status:[$in:active,invited]] /users
}

// Prepare checks a request against the endpoint and renders it for any executor.
func ExampleEndpoint_Prepare() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=10&sortBy=name:asc&search=ann&filter.status=$in:active,invited&filter.age=$gte:18&filter.age=$lt:65", nil)
	req, _ := paginate.ParseRequest(r)

	q, err := usersEndpoint.Prepare(req)
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
	// [users.name ASC users.id ASC] 10 10
}

// A filter expression combines terms with AND, OR, NOT and parentheses, as in nestjs-paginate 15+.
func ExampleEndpoint_Prepare_filterExpression() {
	query := url.Values{"filter": {`(status=$in:active,invited OR age=$gte:65) AND NOT name=$eq:"Ann Lee"`}}
	r := httptest.NewRequest(http.MethodGet, "/users?"+query.Encode(), nil)
	req, _ := paginate.ParseRequest(r)

	q, err := usersEndpoint.Prepare(req)
	if err != nil {
		panic(err)
	}
	fmt.Println(q.Where[0].SQL)
	fmt.Println(q.Where[0].Args...)
	// Output:
	// (((users.status IN (?, ?)) OR (users.age >= ?)) AND (NOT (users.name = ?)))
	// active invited 65 Ann Lee
}

type user struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// In cursor pagination the next page starts after the last row of this one: WHERE takes the place
// of OFFSET.
func ExampleEndpoint_Prepare_cursor() {
	cfg := usersConfig()
	cfg.Pagination = paginate.CursorPagination
	endpoint := paginate.MustCompile(cfg)

	q, err := endpoint.Prepare(&paginate.Request{Limit: 2, SortBy: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}}})
	if err != nil {
		panic(err)
	}
	// The executor reads FetchLimit() rows: one more than the page, to tell whether there is a next.
	rows := []user{
		{ID: 3, Name: "Cid", CreatedAt: time.Date(2024, 5, 3, 10, 0, 0, 0, time.UTC)},
		{ID: 2, Name: "Bob", CreatedAt: time.Date(2024, 5, 2, 10, 0, 0, 0, time.UTC)},
		{ID: 1, Name: "Ann", CreatedAt: time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)},
	}[:q.FetchLimit()]
	page, err := paginate.NewResponse(q, rows, paginate.Total{})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(page.Data), page.Meta.HasNextPage)

	next, err := endpoint.Prepare(&paginate.Request{Limit: 2, SortBy: page.Meta.SortBy, Cursor: page.Meta.NextCursor})
	if err != nil {
		panic(err)
	}
	fmt.Println(next.Where[0].SQL)
	fmt.Println(next.Where[0].Args...)
	// Output:
	// 2 true
	// ((users.created_at <= ?) AND ((users.created_at < ?) OR ((users.created_at = ?) AND (users.id > ?))))
	// 2024-05-02T10:00:00Z 2024-05-02T10:00:00Z 2024-05-02T10:00:00Z 2
}

// The meta of an answer echoes the request, so a client can render its controls from it.
func ExampleNewResponse() {
	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=2&sortBy=name:asc&filter.status=active", nil)
	req, _ := paginate.ParseRequest(r)
	q, err := usersEndpoint.Prepare(req)
	if err != nil {
		panic(err)
	}

	page, err := paginate.NewResponse(q, []string{"Ann", "Bob"}, paginate.Total{Items: 5})
	if err != nil {
		panic(err)
	}
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
	//     "totalPages": 3,
	//     "currentPage": 2,
	//     "hasNextPage": true,
	//     "hasPreviousPage": true,
	//     "sortBy": [
	//       [
	//         "name",
	//         "ASC"
	//       ]
	//     ],
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
	endpoint := paginate.MustCompile(cfg)

	r := httptest.NewRequest(http.MethodGet, "/users?page=2&limit=1&filter.age=$gte:18", nil)
	req, _ := paginate.ParseRequest(r)
	q, err := endpoint.Prepare(req)
	if err != nil {
		panic(err)
	}

	page, _ := paginate.NewResponse(q, []string{"Ann"}, paginate.Total{Items: 3})
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

// Compile reports every mistake of a config at once, before the first request.
func ExampleCompile() {
	_, err := paginate.Compile(paginate.Config{
		Fields: []paginate.Field{
			{Name: "rating", Column: "reviews.rating", Type: paginate.Int, Filter: []paginate.Operator{paginate.OpContains}},
		},
		DefaultSort: []paginate.Sort{{Field: "createdAt", Order: paginate.Desc}},
	})
	fmt.Println(err)
	// Output:
	// paginate: invalid config: field rating: $contains needs an array type, the field is int
	// default sort: unknown field createdAt
	// no sort: set DefaultSort, make a field sortable, or set TieBreak
}

type product struct {
	ID         int64     `json:"id" paginate:"unique"`
	Name       string    `json:"name" paginate:"sort;search;filter:$ilike,$sw"`
	Price      float64   `json:"price" paginate:"sort;filter:$gte,$lte"`
	Status     string    `json:"status" paginate:"filter:$in;enum:active,archived"`
	ReleasedAt time.Time `json:"releasedAt" paginate:"sort"`
	Secret     string    `json:"-"`
}

// ConfigOf reads the fields from paginate tags; the Go types give the field types.
func ExampleConfigOf() {
	cfg, err := paginate.ConfigOf[product]("products")
	if err != nil {
		panic(err)
	}
	for _, field := range cfg.Fields {
		fmt.Println(field.Name, field.Column, field.Type, field.Sortable, field.Searchable, field.Filter)
	}
	fmt.Println(cfg.TieBreak)
	// Output:
	// id products.id int false false []
	// name products.name text true true [$eq $ilike $sw]
	// price products.price number true false [$eq $gte $lte]
	// status products.status enum(active|archived) false false [$eq $in]
	// releasedAt products.released_at time true false []
	// products.id
}

var productStatuses = []string{"active", "archived", "draft"}

// Tags and code describe one endpoint together: ConfigOf reads the tags, code changes and adds the
// fields tags cannot describe, and Compile checks the result as a whole.
func ExampleConfig_Field() {
	cfg, err := paginate.ConfigOf[product]("products")
	if err != nil {
		panic(err)
	}
	cfg.Field("status").Type = paginate.Enum(productStatuses...) // values from Go constants
	cfg.Fields = append(cfg.Fields, paginate.Field{              // a joined column
		Name: "brand", Column: "brands.name", Sortable: true, Searchable: true,
	})
	endpoint := paginate.MustCompile(cfg)

	for _, field := range endpoint.Fields() {
		fmt.Println(field.Name, field.Column, field.Type)
	}
	// Output:
	// id products.id int
	// name products.name text
	// price products.price number
	// status products.status enum(active|archived|draft)
	// releasedAt products.released_at time
	// brand brands.name any
}

// Errors carry a code and the field, and HTTPStatus picks the status to answer them with.
func ExampleHTTPStatus() {
	r := httptest.NewRequest(http.MethodGet, "/users?filter.age=$gte:old", nil)
	req, _ := paginate.ParseRequest(r)
	_, err := usersEndpoint.Prepare(req)

	body, _ := json.Marshal(err)
	fmt.Println(paginate.HTTPStatus(err), string(body))
	// Output:
	// 400 {"code":"invalid_value","field":"age","message":"invalid value \"old\" for age: must be an integer"}
}

// Map turns the rows of an answer into API types and keeps the meta with the links.
func ExampleMap() {
	q, _ := usersEndpoint.Prepare(&paginate.Request{Limit: 2})
	page, _ := paginate.NewResponse(q, []user{{ID: 1, Name: "ann"}, {ID: 2, Name: "bob"}}, paginate.Total{Items: 2})

	names := paginate.Map(page, func(u user) string { return strings.ToUpper(u.Name) })
	fmt.Println(names.Data, names.Meta.TotalItems)
	// Output:
	// [ANN BOB] 2
}
