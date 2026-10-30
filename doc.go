// Package paginate adds pagination, sorting, search and filtering to Go list endpoints, with the
// query language of nestjs-paginate: page, limit, sortBy, search, searchBy, filter.<field>,
// filter=<expression> and cursor, answered with {data, meta} and, optionally, nestjs-paginate's
// meta and links.
//
// The package itself has no dependencies. A Config describes an endpoint: its fields (the column,
// the type and what clients may do with each), how it pages (page numbers or cursors) and counts
// (exactly, by estimate, or not at all). Compile checks it once and builds an Endpoint. Per
// request, ParseRequest (net/http) or ParseQuery (anything else) reads the query string,
// Endpoint.Prepare checks it and renders SQL, an executor runs it, and NewResponse builds the
// answer:
//
//   - gormx runs requests on GORM and builds configs from GORM models;
//   - squirrelx runs them on a squirrel select with pgx;
//   - fasthttpx parses fasthttp requests.
//
// A minimal endpoint:
//
//	var usersEndpoint = paginate.MustCompile(paginate.Config{
//		Fields: []paginate.Field{
//			{Name: "createdAt", Column: "users.created_at", Type: paginate.Time, Sortable: true},
//			{Name: "name", Column: "users.name", Sortable: true, Searchable: true},
//			{Name: "status", Column: "users.status", Type: paginate.Enum("active", "invited"),
//				Filter: []paginate.Operator{paginate.OpIn}},
//		},
//		TieBreak: "users.id",
//	})
//
//	func listUsers(w http.ResponseWriter, r *http.Request) {
//		req, err := paginate.ParseRequest(r)
//		if err != nil {
//			writeError(w, err) // HTTPStatus(err) is 400 for the client's mistakes, 500 otherwise
//			return
//		}
//		page, err := gormx.Paginate[User](r.Context(), *req, usersEndpoint, db.Model(&User{}))
//		if err != nil {
//			writeError(w, err)
//			return
//		}
//		...
//	}
//
// The fields can also come from struct tags (ConfigOf): `paginate:"sort;search;filter:$in"`.
//
// Clients reach only the fields and operators the endpoint lists, values are checked against the
// fields' types and passed as bind parameters, and a unique tie-breaker column ends every ORDER
// BY, so pages are stable. Errors of the client are *Error values with a code and the field they
// are about, ready to be the JSON of a 400.
package paginate
