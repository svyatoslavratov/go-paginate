package paginate

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKeysetExpr(t *testing.T) {
	t.Parallel()
	price := keysetKey{entry: SortEntry{"t.price", Desc}}
	name := keysetKey{entry: SortEntry{"t.name", Asc}}
	id := keysetKey{entry: SortEntry{"t.id", Asc}, notNull: true}
	tests := []struct {
		name      string
		keys      []keysetKey
		boundary  []any
		nullSort  NullSort
		backward  bool
		inclusive bool
		wantSQL   string
		wantArgs  []any
	}{
		{
			name:     "one key and the tie-breaker, with the bound an index scan starts at",
			keys:     []keysetKey{price, id},
			boundary: []any{"9.5", "7"},
			// DESC puts NULL values first in Postgres, so none come after a value.
			wantSQL:  "((t.price <= ?) AND ((t.price < ?) OR ((t.price = ?) AND (t.id > ?))))",
			wantArgs: []any{"9.5", "9.5", "9.5", "7"},
		},
		{
			name:     "NULL values last come after any value",
			keys:     []keysetKey{name, id},
			boundary: []any{"Milo", "7"},
			wantSQL:  "((t.name > ? OR t.name IS NULL) OR ((t.name = ?) AND (t.id > ?)))",
			wantArgs: []any{"Milo", "Milo", "7"},
		},
		{
			name:     "after a NULL that sorts last only equal NULL values with a bigger id",
			keys:     []keysetKey{name, id},
			boundary: []any{nil, "7"},
			wantSQL:  "((t.name IS NULL) AND (t.id > ?))",
			wantArgs: []any{"7"},
		},
		{
			name:     "after a NULL that sorts first comes every value",
			keys:     []keysetKey{price, id},
			boundary: []any{nil, "7"},
			wantSQL:  "((t.price IS NOT NULL) OR ((t.price IS NULL) AND (t.id > ?)))",
			wantArgs: []any{"7"},
		},
		{
			name:     "backward flips the comparisons and the NULL placement",
			keys:     []keysetKey{price, id},
			boundary: []any{"9.5", "7"},
			backward: true,
			wantSQL:  "((t.price > ? OR t.price IS NULL) OR ((t.price = ?) AND (t.id < ?)))",
			wantArgs: []any{"9.5", "9.5", "7"},
		},
		{
			name:     "NullsFirst on an ascending key",
			keys:     []keysetKey{name, id},
			boundary: []any{"Milo", "7"},
			nullSort: NullsFirst,
			wantSQL:  "((t.name >= ?) AND ((t.name > ?) OR ((t.name = ?) AND (t.id > ?))))",
			wantArgs: []any{"Milo", "Milo", "Milo", "7"},
		},
		{
			name:     "a NotNull key needs no NULL checks and gets the bound",
			keys:     []keysetKey{{entry: SortEntry{"t.name", Asc}, notNull: true}, id},
			boundary: []any{"Milo", "7"},
			wantSQL:  "((t.name >= ?) AND ((t.name > ?) OR ((t.name = ?) AND (t.id > ?))))",
			wantArgs: []any{"Milo", "Milo", "Milo", "7"},
		},
		{
			name:     "a single key is a range already",
			keys:     []keysetKey{id},
			boundary: []any{"7"},
			wantSQL:  "t.id > ?",
			wantArgs: []any{"7"},
		},
		{
			name:      "inclusive adds the boundary row",
			keys:      []keysetKey{price, id},
			boundary:  []any{"9.5", "7"},
			backward:  true,
			inclusive: true,
			wantSQL:   "((t.price > ? OR t.price IS NULL) OR ((t.price = ?) AND (t.id < ?)) OR ((t.price = ?) AND (t.id = ?)))",
			wantArgs:  []any{"9.5", "9.5", "7", "9.5", "7"},
		},
		{
			name:     "no row after a lone NULL that sorts last",
			keys:     []keysetKey{{entry: SortEntry{"t.name", Asc}}},
			boundary: []any{nil},
			wantSQL:  "FALSE",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := keysetExpr(tc.keys, tc.boundary, tc.nullSort, tc.backward, tc.inclusive)
			if got.sql != tc.wantSQL {
				t.Fatalf("sql:\nwant %s\ngot  %s", tc.wantSQL, got.sql)
			}
			if !reflect.DeepEqual(got.args, tc.wantArgs) {
				t.Fatalf("args: want %#v, got %#v", tc.wantArgs, got.args)
			}
		})
	}
}

func cursorEndpoint(t *testing.T) *Endpoint {
	t.Helper()
	return compile(t, func(cfg *Config) {
		cfg.Pagination = CursorPagination
		cfg.Fields[1].Filter = nil
	})
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	e := cursorEndpoint(t)
	req := Request{Limit: 2, SortBy: []Sort{{"createdAt", Desc}}}
	q, err := e.Prepare(&req)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Keyset || q.Count != CountNone || q.Offset != 0 || q.FetchLimit() != 3 || len(q.Where) != 0 {
		t.Fatalf("first cursor page: %+v", q)
	}
	created := time.Date(2024, 5, 2, 10, 0, 0, 123456000, time.UTC)
	rows := []map[string]any{
		{"id": json.Number("3"), "createdAt": "2024-05-03T10:00:00Z"},
		{"id": json.Number("2"), "createdAt": created},
		{"id": json.Number("1"), "createdAt": "2024-05-01T10:00:00Z"},
	}
	page, err := NewResponse(q, rows, Total{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 || !page.Meta.HasNextPage || page.Meta.HasPreviousPage || page.Meta.PreviousCursor != "" {
		t.Fatalf("meta %+v", page.Meta)
	}

	next := Request{Limit: 2, SortBy: req.SortBy, Cursor: page.Meta.NextCursor}
	q, err = e.Prepare(&next)
	if err != nil {
		t.Fatal(err)
	}
	want := Condition{
		SQL:  "((users.created_at <= ?) AND ((users.created_at < ?) OR ((users.created_at = ?) AND (users.id > ?))))",
		Args: []any{"2024-05-02T10:00:00.123456Z", "2024-05-02T10:00:00.123456Z", "2024-05-02T10:00:00.123456Z", "2"},
	}
	if !reflect.DeepEqual(q.Where, []Condition{want}) || !reflect.DeepEqual(q.OrderBy(true), []string{"users.created_at DESC", "users.id ASC"}) {
		t.Fatalf("next page: %+v %v", q.Where, q.OrderBy(true))
	}

	// The second page pages back from its first row, in reverse order, and turns the rows back.
	page, err = NewResponse(q, []map[string]any{{"id": 1, "createdAt": "2024-05-01T10:00:00Z"}}, Total{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.HasNextPage || !page.Meta.HasPreviousPage || page.Meta.Cursor != next.Cursor {
		t.Fatalf("second page meta %+v", page.Meta)
	}
	back := Request{Limit: 2, SortBy: req.SortBy, Cursor: page.Meta.PreviousCursor}
	q, err = e.Prepare(&back)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q.OrderBy(true), []string{"users.created_at ASC NULLS LAST", "users.id DESC NULLS FIRST"}) {
		t.Fatalf("backward order %v", q.OrderBy(true))
	}
	page, err = NewResponse(q, []map[string]any{{"id": 2, "createdAt": "b"}, {"id": 3, "createdAt": "c"}, {"id": 4, "createdAt": "d"}}, Total{})
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Data; len(got) != 2 || got[0]["id"] != 3 || got[1]["id"] != 2 || !page.Meta.HasPreviousPage || !page.Meta.HasNextPage {
		t.Fatalf("backward page %v %+v", got, page.Meta)
	}
}

func TestCursorRejects(t *testing.T) {
	t.Parallel()
	e := cursorEndpoint(t)
	sortBy := []Sort{{"createdAt", Desc}}
	q, _ := e.Prepare(&Request{SortBy: sortBy})
	good, err := q.cursor.encodeValues([]any{"2024-05-02T10:00:00Z", 2}, cursorAfter)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(token string) string { return base64.RawURLEncoding.EncodeToString([]byte(token)) }
	tests := map[string]struct {
		cursor string
		sortBy []Sort
		why    string
	}{
		"not base64":          {cursor: "%%%", sortBy: sortBy, why: "not base64url"},
		"not JSON":            {cursor: encode("{"), sortBy: sortBy, why: "malformed"},
		"too few values":      {cursor: encode(`["n","createdAt:DESC,id:ASC",1]`), sortBy: sortBy, why: "malformed"},
		"unknown direction":   {cursor: encode(`["x","createdAt:DESC,id:ASC","2024-05-02T10:00:00Z",2]`), sortBy: sortBy, why: "malformed"},
		"another sort":        {cursor: good, sortBy: []Sort{{"createdAt", Asc}}, why: "another sort"},
		"a value of a type":   {cursor: encode(`["n","createdAt:DESC,id:ASC","yesterday",2]`), sortBy: sortBy, why: "does not fit"},
		"an object value":     {cursor: encode(`["n","createdAt:DESC,id:ASC",{},2]`), sortBy: sortBy, why: "malformed"},
		"trailing data":       {cursor: encode(`["n","createdAt:DESC,id:ASC","2024-05-02T10:00:00Z",2] []`), sortBy: sortBy, why: "malformed"},
		"longer than the cap": {cursor: strings.Repeat("A", maxCursorLength+1), sortBy: sortBy, why: "too long"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := e.Prepare(&Request{SortBy: tc.sortBy, Cursor: tc.cursor})
			wantError(t, err, CodeInvalidCursor, "", tc.why)
		})
	}
	if _, err := e.Prepare(&Request{SortBy: sortBy, Cursor: good}); err != nil {
		t.Fatalf("the good cursor: %v", err)
	}
}

func TestCursorDisabledAndCount(t *testing.T) {
	t.Parallel()
	e := compile(t, func(cfg *Config) {
		cfg.Pagination = CursorPagination
		cfg.AllowDisabledPagination = true
	})
	for _, req := range []Request{clientMode(ModeCount), clientMode(ModeDisabled)} {
		q, err := e.Prepare(&req)
		if err != nil {
			t.Fatal(err)
		}
		if q.Keyset || q.cursor != nil {
			t.Fatalf("mode %s must not page by cursor", q.Mode)
		}
	}
}

type embeddedBase struct {
	ID        int64 `gorm:"column:id"`
	UpdatedAt time.Time
}

type rowValuesRow struct {
	embeddedBase
	Title    string         `json:"title"`
	Price    float64        `db:"price_cents"`
	Rating   *float64       `json:"rating"`
	Note     sql.NullString `json:"note"`
	UID      [16]byte       `json:"uid"`
	Author   rowAuthor      `json:"author"`
	Hidden   string         `json:"-"`
	internal string
}

type rowAuthor struct {
	Name string `json:"name"`
}

func TestRowValues(t *testing.T) {
	t.Parallel()
	updated := time.Date(2024, 5, 1, 10, 0, 0, 5, time.UTC)
	row := rowValuesRow{
		embeddedBase: embeddedBase{ID: 7, UpdatedAt: updated},
		Title:        "Milo",
		Price:        9.5,
		Note:         sql.NullString{String: "hi", Valid: true},
		UID:          [16]byte{0x84, 0x02, 0xe9, 0x9c, 0x5b, 0x52, 0x52, 0x01, 0x60, 0x79, 0x46, 0x15, 0xa8, 0x16, 0x16, 0x83},
		Author:       rowAuthor{Name: "Ann"},
		Hidden:       "secret",
		internal:     "x",
	}
	keys := []keysetKey{
		{field: "title", column: "title"},
		{field: "cost", column: "price_cents"},
		{field: "rating", column: "rating"},
		{field: "note", column: "note"},
		{field: "uid", column: "uid"},
		{field: "author.name", column: ""},
		{field: "", column: "updated_at"},
		{field: "", column: "id"},
		{field: "hiddenField", column: "hidden"},
	}
	got, err := rowValues(&row, keys)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"Milo", 9.5, nil, "hi", "8402e99c-5b52-5201-6079-4615a8161683", "Ann", "2024-05-01T10:00:00.000000005Z", int64(7), "secret"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v\ngot  %#v", want, got)
	}

	if _, err := rowValues(&row, []keysetKey{{field: "missing", column: "missing"}}); err == nil || !strings.Contains(err.Error(), "no field for the sort key missing") {
		t.Fatalf("want an error for a missing field, got %v", err)
	}
	tieBreak := keysetKey{entry: SortEntry{"t.serial", Asc}, column: "serial"}
	if _, err := rowValues(&row, []keysetKey{tieBreak}); err == nil || !strings.Contains(err.Error(), "the sort key t.serial") {
		t.Fatalf("an error about a tie-breaker that is no field must name its column, got %v", err)
	}
	raw := json.RawMessage(`{"title": "Milo", "author": {"name": "Ann"}, "id": 12345678901234567890}`)
	got, err = rowValues(&raw, []keysetKey{{field: "title"}, {field: "author.name"}, {column: "id"}})
	if err != nil || !reflect.DeepEqual(got, []any{"Milo", "Ann", json.Number("12345678901234567890")}) {
		t.Fatalf("JSON rows: %#v %v", got, err)
	}
}

func TestSnakeCase(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"ID": "id", "UserID": "user_id", "SKU": "sku", "ReleasedAt": "released_at", "HTTPServer": "http_server", "Name2": "name2", "createdAt": "created_at",
	} {
		if got := snakeCase(name); got != want {
			t.Errorf("%s: want %s, got %s", name, want, got)
		}
	}
}
