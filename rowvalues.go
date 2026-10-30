package paginate

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// rowValues reads the values of the keyset keys from a row, to make its cursor. A row is a struct
// or a pointer to one, a map[string]any, or a JSON object (json.RawMessage). A key's value is found
// by the field's API name, the way encoding/json names struct fields (nested structs give dotted
// names), and failing that by the last part of its column, the way a db or gorm column tag or the
// snake_case of a Go field name calls it.
func rowValues(row any, keys []keysetKey) ([]any, error) {
	switch r := row.(type) {
	case *json.RawMessage:
		return jsonRowValues(*r, keys)
	case *map[string]any:
		return mapRowValues(*r, keys)
	}

	v := reflect.ValueOf(row)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, errors.New("paginate: cursor pagination: a row is nil")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("paginate: cursor pagination reads structs, maps and JSON objects, not %s", v.Type())
	}
	index := structIndexOf(v.Type())
	values := make([]any, len(keys))
	for i, key := range keys {
		path, ok := index.lookup(key)
		if !ok {
			return nil, fmt.Errorf("paginate: cursor pagination: %s has no field for the sort key %s", v.Type(), key.describe())
		}
		field, err := v.FieldByIndexErr(path)
		if err != nil {
			// A nil embedded pointer: the value is NULL.
			continue
		}
		if values[i], err = cursorValue(field); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func jsonRowValues(row json.RawMessage, keys []keysetKey) ([]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(row))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("paginate: cursor pagination: a row is not a JSON object: %w", err)
	}
	return mapRowValues(object, keys)
}

func mapRowValues(row map[string]any, keys []keysetKey) ([]any, error) {
	values := make([]any, len(keys))
	for i, key := range keys {
		value, ok := mapPath(row, key.field)
		if !ok && key.column != "" {
			value, ok = row[key.column]
		}
		if !ok {
			return nil, fmt.Errorf("paginate: cursor pagination: a row has no value for the sort key %s", key.describe())
		}
		switch value := value.(type) {
		case nil, json.Number:
			values[i] = value
		default:
			var err error
			if values[i], err = cursorValue(reflect.ValueOf(value)); err != nil {
				return nil, err
			}
		}
	}
	return values, nil
}

// mapPath finds a dotted name in nested maps.
func mapPath(row map[string]any, name string) (any, bool) {
	if name == "" {
		return nil, false
	}
	if value, ok := row[name]; ok {
		return value, true
	}
	head, rest, found := strings.Cut(name, ".")
	if !found {
		return nil, false
	}
	nested, ok := row[head].(map[string]any)
	if !ok {
		return nil, false
	}
	return mapPath(nested, rest)
}

var (
	timeType   = reflect.TypeFor[time.Time]()
	valuerType = reflect.TypeFor[driver.Valuer]()
)

// cursorValue turns a field of a row into a value of a cursor: a string, a number, a bool or nil.
// Timestamps keep every digit (RFC 3339 with nanoseconds), driver.Valuer types give their database
// value, 16-byte arrays are UUIDs.
func cursorValue(v reflect.Value) (any, error) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		if v.Kind() == reflect.Pointer && v.Type().Implements(valuerType) {
			break
		}
		v = v.Elem()
	}
	if v.Type() == timeType {
		return v.Interface().(time.Time).Format(time.RFC3339Nano), nil
	}
	if valuer, ok := asValuer(v); ok {
		value, err := valuer.Value()
		if err != nil {
			return nil, fmt.Errorf("paginate: cursor value: %w", err)
		}
		switch value := value.(type) {
		case nil, string, bool, int64, float64:
			return value, nil
		case []byte:
			return string(value), nil
		case time.Time:
			return value.Format(time.RFC3339Nano), nil
		}
		return nil, fmt.Errorf("paginate: cursor pagination cannot encode a %T", value)
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.Array:
		if v.Len() == 16 && v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, 16)
			reflect.Copy(reflect.ValueOf(b), v)
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
		}
	}
	return nil, fmt.Errorf("paginate: cursor pagination cannot encode a %s", v.Type())
}

// asValuer returns the driver.Valuer of a value, a pointer receiver included.
func asValuer(v reflect.Value) (driver.Valuer, bool) {
	if v.Type().Implements(valuerType) {
		valuer, ok := v.Interface().(driver.Valuer)
		return valuer, ok
	}
	if reflect.PointerTo(v.Type()).Implements(valuerType) {
		if !v.CanAddr() {
			copied := reflect.New(v.Type())
			copied.Elem().Set(v)
			v = copied.Elem()
		}
		valuer, ok := v.Addr().Interface().(driver.Valuer)
		return valuer, ok
	}
	return nil, false
}

// structIndex finds the fields of a struct type by API name and by column.
type structIndex struct {
	// byName holds encoding/json names, dotted for fields of nested structs.
	byName map[string]indexEntry
	// byColumn holds column names: db tags, gorm column tags, the snake_case of Go names.
	byColumn map[string]indexEntry
}

type indexEntry struct {
	path  []int
	depth int
}

// maxStructDepth bounds the nesting structIndex walks into.
const maxStructDepth = 4

var structIndexes sync.Map

func structIndexOf(t reflect.Type) *structIndex {
	if cached, ok := structIndexes.Load(t); ok {
		return cached.(*structIndex)
	}
	index := &structIndex{byName: make(map[string]indexEntry), byColumn: make(map[string]indexEntry)}
	index.walk(t, nil, "", 0)
	cached, _ := structIndexes.LoadOrStore(t, index)
	return cached.(*structIndex)
}

// walk indexes the fields of t. Embedded structs without a JSON name are promoted, as in
// encoding/json; a shallower field wins over a deeper one with the same name.
func (x *structIndex) walk(t reflect.Type, path []int, prefix string, depth int) {
	if depth > maxStructDepth {
		return
	}
	for i := range t.NumField() {
		sf := t.Field(i)
		fieldPath := append(slices.Clone(path), i)
		jsonName, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		fieldType := sf.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		// Exported fields of embedded structs are promoted, the struct type exported or not, as
		// encoding/json promotes them; other unexported fields cannot be read.
		if sf.Anonymous && jsonName == "" && fieldType.Kind() == reflect.Struct {
			x.walk(fieldType, fieldPath, prefix, depth+1)
			continue
		}
		if !sf.IsExported() {
			continue
		}
		if jsonName != "-" {
			name := jsonName
			if name == "" {
				name = sf.Name
			}
			x.add(x.byName, prefix+name, fieldPath, depth)
			if fieldType.Kind() == reflect.Struct && !isValueStruct(fieldType) {
				x.walk(fieldType, fieldPath, prefix+name+".", depth+1)
			}
		}
		if prefix == "" {
			for _, column := range columnNamesOf(sf) {
				x.add(x.byColumn, column, fieldPath, depth)
			}
		}
	}
}

func (x *structIndex) add(index map[string]indexEntry, name string, path []int, depth int) {
	if existing, ok := index[name]; !ok || depth < existing.depth {
		index[name] = indexEntry{path: path, depth: depth}
	}
}

func (x *structIndex) lookup(key keysetKey) ([]int, bool) {
	if entry, ok := x.byName[key.field]; ok && key.field != "" {
		return entry.path, true
	}
	if entry, ok := x.byColumn[key.column]; ok && key.column != "" {
		return entry.path, true
	}
	return nil, false
}

// isValueStruct reports structs that hold one value rather than fields: time.Time, driver.Valuer
// types such as sql.NullString.
func isValueStruct(t reflect.Type) bool {
	return t == timeType || t.Implements(valuerType) || reflect.PointerTo(t).Implements(valuerType)
}

// columnNamesOf returns the column names a struct field may stand for: its db tag, its gorm column
// tag and the snake_case of its name.
func columnNamesOf(sf reflect.StructField) []string {
	names := make([]string, 0, 3)
	if db, _, _ := strings.Cut(sf.Tag.Get("db"), ","); db != "" && db != "-" {
		names = append(names, db)
	}
	if column := gormColumn(sf.Tag.Get("gorm")); column != "" {
		names = append(names, column)
	}
	return append(names, snakeCase(sf.Name))
}

// gormColumn reads column:<name> from a gorm tag.
func gormColumn(tag string) string {
	for setting := range strings.SplitSeq(tag, ";") {
		key, value, found := strings.Cut(setting, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), "column") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// snakeCase names a Go field the way GORM names its column: ReleasedAt is released_at, UserID is
// user_id, SKU is sku, HTTPServer is http_server.
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			startsWord := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) ||
				unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1]))
			if startsWord {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
