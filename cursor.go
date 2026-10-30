package paginate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// A cursor marks a row in cursor pagination: the values of the row's sort keys, the sort they
// belong to, and the direction to page in. It is base64url-encoded JSON, opaque to clients:
//
//	["n", "price:DESC,id:ASC", 19.99, 42]
//
// n pages forward, to the rows after the marked one; p pages back, to the rows before it; P pages
// back through it, to the rows before it and the row itself. The sort ties a cursor to the request
// it came from: a cursor sent with another sort is rejected, not misread. ~ names a tie-breaker
// that is no field.

// maxCursorLength caps the cursors Prepare decodes, in bytes.
const maxCursorLength = 4096

// cursorDirection is where a cursor leads from the row it marks.
type cursorDirection string

const (
	// cursorAfter leads to the rows after the row.
	cursorAfter cursorDirection = "n"
	// cursorBefore leads to the rows before the row.
	cursorBefore cursorDirection = "p"
	// cursorBackThrough leads to the rows before the row and the row itself: the way back from an
	// empty page.
	cursorBackThrough cursorDirection = "P"
)

// keyset is the cursor pagination of a query: its keys and the boundary of the request's cursor.
type keyset struct {
	keys      []keysetKey
	signature string
	// boundary holds the values of the request's cursor, nil on the first page.
	boundary []any
}

// keysetKey is a key of the keyset and where rows keep its value.
type keysetKey struct {
	entry SortEntry
	// field is the API name, empty for a tie-breaker that is no field.
	field string
	// column is the name rows may use instead: the last part of the column.
	column string
	typ    Type
	// notNull marks a key without NULL values to step around: the tie-breaker, or a NotNull field.
	notNull bool
}

// name is the key in the signature of a cursor, which reaches clients: the API field, or ~ for a
// tie-breaker that is no field, so the cursor never names a column.
func (k keysetKey) name() string {
	if k.field != "" {
		return k.field
	}
	return "~"
}

// describe names the key for the server's errors: the API field or the column.
func (k keysetKey) describe() string {
	if k.field != "" {
		return k.field
	}
	return k.entry.Column
}

// applyCursor turns q into a cursor page: no offset, no count, the keyset condition of the
// request's cursor in Where.
func (e *Endpoint) applyCursor(q *Query, cursor string, keys []sortKey) error {
	ks := e.newKeyset(keys)
	q.Keyset = true
	q.Offset = 0
	q.Count = CountNone
	q.cursor = ks
	if cursor == "" {
		return nil
	}
	direction, values, err := ks.decode(cursor)
	if err != nil {
		return err
	}
	q.backward = direction != cursorAfter
	ks.boundary = values
	q.Where = append(q.Where, condition(keysetExpr(ks.keys, values, e.cfg.NullSort, q.backward, direction == cursorBackThrough)))
	return nil
}

// newKeyset makes the keyset of a sort: its keys and the tie-breaker.
func (e *Endpoint) newKeyset(keys []sortKey) *keyset {
	ks := &keyset{keys: make([]keysetKey, 0, len(keys)+1)}
	for _, key := range keys {
		ks.keys = append(ks.keys, keysetKey{
			entry:   SortEntry{Column: key.field.Column, Order: key.order},
			field:   key.field.Name,
			column:  columnName(key.field.Column),
			typ:     key.field.Type,
			notNull: key.field.NotNull,
		})
	}
	tieBreak := e.cfg.TieBreak
	if i := slices.IndexFunc(ks.keys, func(k keysetKey) bool { return k.entry.Column == tieBreak }); i >= 0 {
		ks.keys[i].notNull = true
	} else {
		key := keysetKey{entry: SortEntry{Column: tieBreak, Order: Asc}, field: e.tieBreakField, column: columnName(tieBreak), notNull: true}
		if field := e.fields[e.tieBreakField]; field != nil {
			key.typ = field.Type
		}
		ks.keys = append(ks.keys, key)
	}
	names := make([]string, len(ks.keys))
	for i, key := range ks.keys {
		names[i] = key.name() + ":" + string(key.entry.Order)
	}
	ks.signature = strings.Join(names, ",")
	return ks
}

// encode makes the cursor of a row, leading in direction.
func (ks *keyset) encode(row any, direction cursorDirection) (string, error) {
	values, err := rowValues(row, ks.keys)
	if err != nil {
		return "", err
	}
	return ks.encodeValues(values, direction)
}

func (ks *keyset) encodeValues(values []any, direction cursorDirection) (string, error) {
	token := append([]any{string(direction), ks.signature}, values...)
	data, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("paginate: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// decode reads a cursor of this keyset. Values come back as strings, booleans and nils, ready to be
// arguments.
func (ks *keyset) decode(cursor string) (direction cursorDirection, values []any, err error) {
	invalid := func(why string) error {
		return invalidRequest(CodeInvalidCursor, "", "invalid cursor: %s", why)
	}
	if len(cursor) > maxCursorLength {
		return "", nil, invalid("too long")
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", nil, invalid("not base64url")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var token []any
	if err := decoder.Decode(&token); err != nil || decoder.More() || len(token) != len(ks.keys)+2 {
		return "", nil, invalid("malformed")
	}
	switch name, _ := token[0].(string); cursorDirection(name) {
	case cursorAfter, cursorBefore, cursorBackThrough:
		direction = cursorDirection(name)
	default:
		return "", nil, invalid("malformed")
	}
	if signature, _ := token[1].(string); signature != ks.signature {
		return "", nil, invalid("it was made for another sort")
	}

	values = make([]any, len(ks.keys))
	for i, raw := range token[2:] {
		var text string
		switch value := raw.(type) {
		case nil:
			continue
		case string:
			text = value
			values[i] = value
		case json.Number:
			text = value.String()
			values[i] = text
		case bool:
			text = strconv.FormatBool(value)
			values[i] = value
		default:
			return "", nil, invalid("malformed")
		}
		if !cursorAccepts(ks.keys[i].typ, text) {
			return "", nil, invalid("a value does not fit its field")
		}
	}
	return direction, values, nil
}

// cursorAccepts checks a value of a cursor against the field's type. A row may hold what a filter
// may not: any value of an enum column, a timestamp in a column the API calls a date.
func cursorAccepts(t Type, value string) bool {
	switch t.kind {
	case enumKind, arrayKind:
		return true
	case dateKind:
		return Time.accepts(value)
	}
	return t.accepts(value)
}

// keysetExpr is the condition for the rows after the boundary in the order of the keys:
//
//	k1 after v1 OR (k1 = v1 AND k2 after v2) OR (k1 = v1 AND k2 = v2 AND k3 after v3) ...
//
// with NULL values where the keys put them. Backward, it is the rows before the boundary: the same
// with every key reversed. Inclusive adds the boundary row itself.
//
// Postgres reads such an OR as a filter, not as a range of an index, so a deep page would scan
// everything before it. The condition is therefore ANDed with a bound on the first key that says
// the same thing as a range (k1 >= v1 going up, k1 <= v1 going down), where Postgres can start an
// index scan. It holds only when no NULL value of k1 follows v1.
func keysetExpr(keys []keysetKey, boundary []any, nullSort NullSort, backward, inclusive bool) sqlExpr {
	terms := make([]sqlExpr, 0, len(keys)+1)
	for i, key := range keys {
		after, ok := afterExpr(key, boundary[i], nullSort, backward)
		if !ok {
			continue
		}
		parts := make([]sqlExpr, 0, i+1)
		for j := range i {
			parts = append(parts, equalExpr(keys[j].entry.Column, boundary[j]))
		}
		terms = append(terms, joinExprs(append(parts, after), " AND "))
	}
	if inclusive {
		parts := make([]sqlExpr, len(keys))
		for i, key := range keys {
			parts[i] = equalExpr(key.entry.Column, boundary[i])
		}
		terms = append(terms, joinExprs(parts, " AND "))
	}
	if len(terms) == 0 {
		return sqlExpr{sql: "FALSE"}
	}
	expr := joinExprs(terms, " OR ")
	if bound, ok := leadingBound(keys, boundary, nullSort, backward); ok {
		return joinExprs([]sqlExpr{bound, expr}, " AND ")
	}
	return expr
}

// leadingBound is the range of the first key the rows after the boundary lie in, when it is
// one: the value is not NULL and no NULL value comes after it. A single key is a range already.
func leadingBound(keys []keysetKey, boundary []any, nullSort NullSort, backward bool) (sqlExpr, bool) {
	key, value := keys[0], boundary[0]
	order, last := key.entry.Order, nullsLast(key.entry.Order, nullSort)
	if backward {
		order, last = order.reverse(), !last
	}
	if len(keys) == 1 || value == nil || last && !key.notNull {
		return sqlExpr{}, false
	}
	comparison := " >= ?"
	if order == Desc {
		comparison = " <= ?"
	}
	return sqlExpr{sql: key.entry.Column + comparison, args: []any{value}}, true
}

// equalExpr is "column holds value", NULL included.
func equalExpr(column string, value any) sqlExpr {
	if value == nil {
		return sqlExpr{sql: column + " IS NULL"}
	}
	return sqlExpr{sql: column + " = ?", args: []any{value}}
}

// afterExpr is "the column comes after value" in the direction of the key; ok is false when no
// value can, after a NULL that sorts last.
func afterExpr(key keysetKey, value any, nullSort NullSort, backward bool) (expr sqlExpr, ok bool) {
	column := key.entry.Column
	order, last := key.entry.Order, nullsLast(key.entry.Order, nullSort)
	if backward {
		order, last = order.reverse(), !last
	}
	if value == nil {
		if last {
			return sqlExpr{}, false
		}
		return sqlExpr{sql: column + " IS NOT NULL"}, true
	}
	comparison := column + " > ?"
	if order == Desc {
		comparison = column + " < ?"
	}
	if last && !key.notNull {
		return sqlExpr{sql: "(" + comparison + " OR " + column + " IS NULL)", args: []any{value}, grouped: true}, true
	}
	return sqlExpr{sql: comparison, args: []any{value}}, true
}
