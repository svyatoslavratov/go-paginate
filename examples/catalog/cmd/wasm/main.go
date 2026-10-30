//go:build js && wasm

// Command wasm is the go-paginate demo for the browser: the same library and endpoints as the
// server, compiled to WebAssembly. It renders the SQL for PGlite (Postgres in WebAssembly) and
// builds the answers; the page runs the SQL. The page reaches it through the global goPaginate
// object.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"syscall/js"

	"github.com/Masterminds/squirrel"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/examples/catalog"
)

func main() {
	api := js.Global().Get("Object").New()
	api.Set("schema", js.FuncOf(func(js.Value, []js.Value) any { return encode(catalog.Fields()) }))
	api.Set("seed", js.FuncOf(func(js.Value, []js.Value) any { return catalog.SeedSQL }))
	api.Set("prepare", js.FuncOf(prepare))
	api.Set("estimate", js.FuncOf(estimate))
	api.Set("respond", js.FuncOf(respond))
	js.Global().Set("goPaginate", api)
	js.Global().Call("dispatchEvent", js.Global().Get("Event").New("go-paginate-ready"))
	select {}
}

type statement struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
	Text string `json:"text"`
}

// prepare(query) renders what the server runs for a query string: the estimate, the count and the
// page, each only when the request needs it.
func prepare(_ js.Value, args []js.Value) any {
	q, err := parse(args[0].String())
	if err != nil {
		return encodeError(err)
	}
	qb := squirrel.Select(catalog.BrowserColumns...).From("products")
	for _, condition := range q.Where {
		qb = qb.Where(squirrel.Expr(condition.SQL, condition.Args...))
	}
	answer := map[string]any{"exactCountBelow": q.ExactCountBelow}
	if q.Count != paginate.CountNone {
		if answer["count"], err = render(squirrel.Select("COUNT(*)::int AS total").FromSelect(qb, "filtered"), ""); err != nil {
			return encodeError(err)
		}
	}
	if q.Count == paginate.CountEstimate {
		if answer["estimate"], err = render(qb, paginate.ExplainPrefix); err != nil {
			return encodeError(err)
		}
	}
	if q.Mode != paginate.ModeCount {
		page := qb.OrderBy(q.OrderBy(true)...)
		if q.Mode == paginate.ModeDefault {
			page = page.Limit(q.FetchLimit()).Offset(q.Offset)
		}
		if answer["page"], err = render(page, ""); err != nil {
			return encodeError(err)
		}
	}
	return encode(answer)
}

// estimate(plan) reads the row estimate from the output of EXPLAIN (FORMAT JSON); -1 if it cannot.
func estimate(_ js.Value, args []js.Value) any {
	n, err := paginate.EstimateFromPlan([]byte(args[0].String()))
	if err != nil {
		return -1
	}
	return float64(n)
}

// respond(query, rows, total, count) builds the answer for the rows the page read; count is how
// it counted the total: exact, estimate or none.
func respond(_ js.Value, args []js.Value) any {
	q, err := parse(args[0].String())
	if err != nil {
		return encodeError(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal([]byte(args[1].String()), &rows); err != nil {
		return encodeError(err)
	}
	total := paginate.Total{Items: uint64(args[2].Int()), Count: paginate.CountExact}
	if args[3].String() == "estimate" {
		total.Count = paginate.CountEstimate
	}
	page, err := paginate.NewResponse(q, rows, total)
	if err != nil {
		return encodeError(err)
	}
	return encode(page)
}

// parse reads a query string the way the server does and prepares it on the endpoint the demo's
// own parameters pick.
func parse(query string) (*paginate.Query, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	req, err := paginate.ParseQuery(values, catalog.Path)
	if err != nil {
		return nil, err
	}
	return catalog.Endpoint(catalog.OptionsOf(values)).Prepare(req)
}

// render renders a statement for PGlite, and a text for the page with the values listed after it.
func render(qb squirrel.SelectBuilder, prefix string) (statement, error) {
	sql, args, err := qb.PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return statement{}, err
	}
	if args == nil {
		args = []any{} // PGlite takes an array of parameters, not null
	}
	text := prefix + sql
	if len(args) != 0 {
		values := make([]string, len(args))
		for i, arg := range args {
			values[i] = fmt.Sprintf("$%d = '%v'", i+1, arg)
		}
		text += "\n-- " + strings.Join(values, ", ")
	}
	return statement{SQL: prefix + sql, Args: args, Text: text}, nil
}

func encode(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return encodeError(err)
	}
	return string(encoded)
}

// encodeError answers like the server: a client's mistake with its code, field and message.
func encodeError(err error) string {
	status := paginate.HTTPStatus(err)
	var body any = map[string]string{"code": "internal", "message": err.Error()}
	var pe *paginate.Error
	if errors.As(err, &pe) {
		body = pe
	}
	if status != http.StatusBadRequest {
		status = http.StatusInternalServerError
	}
	encoded, _ := json.Marshal(map[string]any{"error": body, "status": status})
	return string(encoded)
}
