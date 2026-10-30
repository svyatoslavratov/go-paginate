// Package paginate adds pagination, sorting, search and filtering to Go list endpoints, with the
// query language of nestjs-paginate: page, limit, sortBy, search, searchBy, filter.<field> and
// filter=<expression>, answered with {data, meta} and, optionally, nestjs-paginate's meta and
// links.
//
// The package itself has no dependencies. It parses requests (ParseRequest for net/http,
// ParseQuery for anything else), checks them against a Config and renders them for SQL (Prepare).
// Executors run the result:
//
//   - gormx runs it on GORM and builds configs from models;
//   - squirrelx runs it on a squirrel select with pgx;
//   - fasthttpx parses fasthttp requests.
//
// A minimal endpoint:
//
//	var usersPage = func() paginate.Config {
//		cfg := paginate.NewConfig("users", []string{"users.created_at", "users.name"}, map[string]string{
//			"createdAt": "users.created_at",
//			"name":      "users.name",
//			"status":    "users.status",
//		})
//		cfg.FilterableColumns = paginate.FilterableColumns{"users.status": {paginate.OpIn}}
//		cfg.TieBreakColumn = "users.id"
//		return *cfg
//	}()
//
//	func listUsers(w http.ResponseWriter, r *http.Request) {
//		req, err := paginate.ParseRequest(r)
//		if err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		page, err := gormx.Paginate[User](r.Context(), *req, usersPage, db.Model(&User{}))
//		if errors.Is(err, paginate.ErrInvalidRequest) {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		...
//	}
//
// Clients reach only the fields, columns and operators the Config lists, every value is a bind
// parameter, and a unique tie-breaker column ends every ORDER BY, so pages are stable.
package paginate
