package paginate

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ConfigOf builds a config from the paginate tags of struct T. Tags read like gorm tags, settings
// separated by semicolons:
//
//	type Product struct {
//		ID         int64     `json:"id" paginate:"unique"`
//		Name       string    `json:"name" paginate:"sort;search;filter:$ilike,$sw"`
//		Price      float64   `json:"price" paginate:"sort;filter:$gte,$lte,$btw"`
//		Status     string    `json:"status" paginate:"filter:$in;enum:active,blocked"`
//		Tags       []string  `json:"tags" paginate:"filter:$contains"`
//		ReleasedAt time.Time `json:"releasedAt" paginate:"sort;filter:$btw"`
//	}
//
//	cfg, err := paginate.ConfigOf[Product]("products")
//	if err != nil { ... }
//	cfg.DefaultSort = []paginate.Sort{{Field: "releasedAt", Order: paginate.Desc}}
//	products := paginate.MustCompile(cfg)
//
// The settings:
//
//   - sort, search: the field is sortable, searchable;
//   - filter: it is filterable with $eq; filter:$in,$gt with these operators too; filter:* with
//     every operator;
//   - type:int (also number, text, bool, time, date, uuid, and []int and the like for arrays),
//     enum:a,b,c: its Type, an array of the enum on a slice; without them the Go type decides
//     (int64 is Int, time.Time is Time, []string is ArrayOf(Text), sql.NullInt64 is Int, a 16-byte
//     array is UUID);
//   - column:expr: its column, taken as is; without it, table.column, where column is the db
//     tag, the gorm column tag, or the field name in snake_case;
//   - name:apiName: its API name; without it, the JSON name;
//   - notnull: the column has no NULL values (Field.NotNull);
//   - unique: its column is the TieBreak.
//
// Only fields with a paginate tag become fields; paginate:"" adds one that allows nothing, for the
// default sort or the tie-breaker. Fields of embedded structs are promoted, as encoding/json
// promotes them. Fields of nested structs get dotted names (author.name) and need column:. Errors
// wrap ErrInvalidConfig and name every bad tag.
func ConfigOf[T any](table string) (Config, error) {
	return StructConfig(reflect.TypeFor[T](), func(path []reflect.StructField) (string, error) {
		for _, parent := range path[:len(path)-1] {
			if !parent.Anonymous {
				return "", errors.New("a field of a nested struct needs a column setting")
			}
		}
		name := columnNamesOf(path[len(path)-1])[0]
		if table == "" {
			return name, nil
		}
		return table + "." + name, nil
	})
}

// StructConfig is ConfigOf for packages that know the columns better: column returns the column of
// a struct field from its path, the struct's own field first. gormx.ConfigOf reads the columns from
// the GORM schema.
func StructConfig(t reflect.Type, column func(path []reflect.StructField) (string, error)) (Config, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return Config{}, invalidConfig("%s is not a struct", t)
	}
	var (
		cfg  Config
		errs []error
	)
	walkTags(t, nil, "", 0, func(path []reflect.StructField, name, tag string) {
		field, unique, err := tagField(path, name, tag, column)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.%s: %w", t.Name(), goPath(path), err))
			return
		}
		cfg.Fields = append(cfg.Fields, field)
		if unique && cfg.TieBreak == "" {
			cfg.TieBreak = field.Column
		}
	})
	if len(errs) != 0 {
		return Config{}, invalidConfig("%s", errors.Join(errs...))
	}
	return cfg, nil
}

// walkTags calls visit for every field with a paginate tag, with its path and its JSON name.
func walkTags(t reflect.Type, path []reflect.StructField, prefix string, depth int, visit func(path []reflect.StructField, name, tag string)) {
	if depth > maxStructDepth {
		return
	}
	for i := range t.NumField() {
		sf := t.Field(i)
		fieldPath := append(slices.Clone(path), sf)
		jsonName, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		fieldType := sf.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		tag, tagged := sf.Tag.Lookup("paginate")
		switch {
		case !sf.IsExported() && !sf.Anonymous, tag == "-":
		case sf.Anonymous && jsonName == "" && fieldType.Kind() == reflect.Struct && !tagged:
			walkTags(fieldType, fieldPath, prefix, depth+1, visit)
		case tagged:
			name := jsonName
			if name == "" || name == "-" {
				name = sf.Name
			}
			visit(fieldPath, prefix+name, tag)
		case jsonName != "-" && fieldType.Kind() == reflect.Struct && !isValueStruct(fieldType):
			name := jsonName
			if name == "" {
				name = sf.Name
			}
			walkTags(fieldType, fieldPath, prefix+name+".", depth+1, visit)
		}
	}
}

// tagField makes the field of a tag.
func tagField(path []reflect.StructField, name, tag string, column func(path []reflect.StructField) (string, error)) (field Field, unique bool, err error) {
	field = Field{Name: name, Type: TypeOf(path[len(path)-1].Type)}
	columnSetting := ""
	for setting := range strings.SplitSeq(tag, ";") {
		key, value, _ := strings.Cut(setting, ":")
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "":
		case "sort":
			field.Sortable = true
		case "search":
			field.Searchable = true
		case "filter":
			field.Filter = []Operator{OpEq}
			if value == "*" {
				field.Filter = slices.Clone(AllOperators)
			} else if value != "" {
				for _, operator := range splitSetting(value) {
					if Operator(operator) != OpEq {
						field.Filter = append(field.Filter, Operator(operator))
					}
				}
			}
		case "type":
			if field.Type, err = parseTypeName(value); err != nil {
				return Field{}, false, err
			}
		case "enum":
			// On a slice the values are the elements': an array of the enum.
			if field.Type = Enum(splitSetting(value)...); TypeOf(path[len(path)-1].Type).IsArray() {
				field.Type = ArrayOf(field.Type)
			}
		case "column":
			columnSetting = value
		case "name":
			field.Name = value
		case "unique":
			unique = true
		case "notnull":
			field.NotNull = true
		default:
			return Field{}, false, fmt.Errorf("unknown paginate setting %q", key)
		}
	}
	if columnSetting != "" {
		field.Column = columnSetting
	} else if field.Column, err = column(path); err != nil {
		return Field{}, false, err
	}
	return field, unique, nil
}

func splitSetting(value string) []string {
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// parseTypeName reads the type: setting.
func parseTypeName(name string) (Type, error) {
	if elem, ok := strings.CutPrefix(name, "[]"); ok {
		t, err := parseTypeName(elem)
		return ArrayOf(t), err
	}
	switch strings.ToLower(name) {
	case "text", "string":
		return Text, nil
	case "int", "integer":
		return Int, nil
	case "number", "numeric", "decimal", "float":
		return Number, nil
	case "bool", "boolean":
		return Bool, nil
	case "time", "timestamp":
		return Time, nil
	case "date":
		return Date, nil
	case "uuid":
		return UUID, nil
	}
	return Type{}, fmt.Errorf("unknown type %q", name)
}

// TypeOf is the Type of values of a Go type: Int for integers, Number for floats, Text for
// strings, Bool, Time for time.Time, UUID for 16-byte arrays, ArrayOf for slices; pointers and
// sql.Null types give the type they hold. It is the zero Type for anything else.
func TypeOf(t reflect.Type) Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return Time
	case t.Kind() == reflect.Array && t.Len() == 16 && t.Elem().Kind() == reflect.Uint8:
		return UUID
	case t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8:
		return ArrayOf(TypeOf(t.Elem()))
	case t.Kind() == reflect.Struct:
		// sql.NullString, sql.Null[T] and the like: Valid and the value.
		if valid, ok := t.FieldByName("Valid"); ok && valid.Type.Kind() == reflect.Bool && t.NumField() == 2 {
			for i := range 2 {
				if f := t.Field(i); f.Name != "Valid" {
					return TypeOf(f.Type)
				}
			}
		}
		return Type{}
	}
	switch t.Kind() {
	case reflect.String:
		return Text
	case reflect.Bool:
		return Bool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Int
	case reflect.Float32, reflect.Float64:
		return Number
	}
	return Type{}
}

// goPath names a struct field path for errors: Author.Name.
func goPath(path []reflect.StructField) string {
	names := make([]string, len(path))
	for i, field := range path {
		names[i] = field.Name
	}
	return strings.Join(names, ".")
}
