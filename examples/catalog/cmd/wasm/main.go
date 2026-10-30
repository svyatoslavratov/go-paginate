//go:build js && wasm

// Command wasm is the go-paginate demo for the browser: the same library and config as the server,
// compiled to WebAssembly. It renders SQL for PGlite (Postgres in WebAssembly) and builds the
// answers; the page runs the SQL. The page reaches it through the global goPaginate object.
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

// prepare(query) renders the count and the page of a query string, as the server would run them.
func prepare(_ js.Value, args []js.Value) any {
	req, q, err := parse(args[0].String())
	if err != nil {
		return encodeError(err)
	}
	qb := squirrel.Select(catalog.BrowserColumns...).From("products")
	for _, condition := range q.Where {
		qb = qb.Where(squirrel.Expr(condition.SQL, condition.Args...))
	}
	count, err := render(squirrel.Select("COUNT(*)::int AS total").FromSelect(qb, "filtered"))
	if err != nil {
		return encodeError(err)
	}
	answer := map[string]any{"count": count, "mode": req.Mode}
	if q.Mode != paginate.ModeCount {
		page := qb.OrderBy(q.OrderBy(true)...)
		if q.Mode == paginate.ModeDefault {
			page = page.Limit(q.Limit).Offset(q.Offset)
		}
		selected, err := render(page)
		if err != nil {
			return encodeError(err)
		}
		answer["select"] = selected
	}
	return encode(answer)
}

// respond(query, rows, total) builds the answer for rows the page fetched.
func respond(_ js.Value, args []js.Value) any {
	req, _, err := parse(args[0].String())
	if err != nil {
		return encodeError(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal([]byte(args[1].String()), &rows); err != nil {
		return encodeError(err)
	}
	return encode(paginate.NewResponse(rows, *req, uint64(args[2].Int())))
}

func parse(query string) (*paginate.Request, *paginate.Query, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, nil, err
	}
	req, err := paginate.ParseQuery(values, catalog.Path)
	if err != nil {
		return nil, nil, err
	}
	q, err := paginate.Prepare(req, catalog.ConfigFor(values.Get("format")))
	if err != nil {
		return nil, nil, err
	}
	return req, q, nil
}

func render(qb squirrel.SelectBuilder) (statement, error) {
	sql, args, err := qb.PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return statement{}, err
	}
	if args == nil {
		args = []any{} // PGlite takes an array of parameters, not null
	}
	text := sql
	if len(args) != 0 {
		values := make([]string, len(args))
		for i, arg := range args {
			values[i] = fmt.Sprintf("$%d = '%v'", i+1, arg)
		}
		text += "\n-- " + strings.Join(values, ", ")
	}
	return statement{SQL: sql, Args: args, Text: text}, nil
}

func encode(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return encodeError(err)
	}
	return string(encoded)
}

func encodeError(err error) string {
	status := http.StatusInternalServerError
	if errors.Is(err, paginate.ErrInvalidRequest) {
		status = http.StatusBadRequest
	}
	encoded, _ := json.Marshal(map[string]any{"error": err.Error(), "status": status})
	return string(encoded)
}
