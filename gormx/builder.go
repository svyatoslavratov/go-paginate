package gormx

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/svyatoslavratov/go-paginate"
)

// Builder builds a paginate.Config from pointers to model and DTO fields instead of hand-written
// names: &model.Title becomes the column "templates.title" through the GORM schema, &dto.Title its
// JSON name, and the Go type of the model field the field's Type. A renamed field then breaks the
// build, not a request. Use it when the model and the API type differ; ConfigOf reads tags from one
// struct.
//
//	var model Template
//	var dto TemplateDTO
//	b := gormx.NewBuilder(db, "templates", &model, &dto)
//	b.Field(&dto.ID, &model.ID)
//	b.Field(&dto.Title, &model.Title).Sortable().Searchable().Filter(paginate.OpILike, paginate.OpSw)
//	b.Field(&dto.CreatedAt, &model.CreatedAt).Sortable().Filter(paginate.OpBtw)
//	b.DefaultSort(&dto.CreatedAt, paginate.Desc)
//	cfg, err := b.Config()
type Builder struct {
	db          *gorm.DB
	table       string
	modelPtr    any
	dtoPtr      any
	modelSchema *schema.Schema
	fields      []paginate.Field
	defaultSort []paginate.Sort
	tieBreak    string
	errs        []error
}

// NewBuilder starts a config for table, the table or alias the query selects from. modelPtr and
// dtoPtr must be pointers to structs, the same pointer when the model is the API type; the fields
// passed to the methods must belong to them. db supplies the naming strategy and is never queried.
// The tie-breaker is the model's primary key when it is a single column.
func NewBuilder(db *gorm.DB, table string, modelPtr, dtoPtr any) *Builder {
	b := &Builder{db: db, table: table, modelPtr: modelPtr, dtoPtr: dtoPtr}
	switch {
	case db == nil:
		b.errs = append(b.errs, errors.New("builder: gorm db is required"))
	case modelPtr == nil:
		b.errs = append(b.errs, errors.New("builder: model is required"))
	default:
		parsed, err := schema.Parse(modelPtr, &sync.Map{}, db.NamingStrategy)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("builder: parse model %T: %w", modelPtr, err))
		}
		b.modelSchema = parsed
	}
	b.tieBreak = primaryKeyColumn(table, b.modelSchema)
	return b
}

// FieldBuilder sets what clients may do with a field of a Builder.
type FieldBuilder struct {
	b *Builder
	// index is the field in b.fields, -1 for a field that did not resolve.
	index int
}

// Field adds the API field named by the JSON name of dtoField and reading the column of
// modelField. A pointer that does not resolve leaves the field out and is reported by Config.
func (b *Builder) Field(dtoField, modelField any) *FieldBuilder {
	name := b.JSONName(dtoField)
	column, goType := b.column(modelField)
	if name == "" || column == "" {
		return &FieldBuilder{b: b, index: -1}
	}
	b.fields = append(b.fields, paginate.Field{Name: name, Column: column, Type: paginate.TypeOf(goType)})
	if b.modelSchema != nil {
		markNotNull(b.fields[len(b.fields)-1:], b.table, b.modelSchema)
	}
	return &FieldBuilder{b: b, index: len(b.fields) - 1}
}

func (f *FieldBuilder) set(change func(field *paginate.Field)) *FieldBuilder {
	if f.index >= 0 {
		change(&f.b.fields[f.index])
	}
	return f
}

// Sortable lets clients sort by the field.
func (f *FieldBuilder) Sortable() *FieldBuilder {
	return f.set(func(field *paginate.Field) { field.Sortable = true })
}

// Searchable makes the field part of ?search=.
func (f *FieldBuilder) Searchable() *FieldBuilder {
	return f.set(func(field *paginate.Field) { field.Searchable = true })
}

// Filter makes the field filterable with $eq and the operators; paginate.AllOperators allows every
// operator.
func (f *FieldBuilder) Filter(operators ...paginate.Operator) *FieldBuilder {
	return f.set(func(field *paginate.Field) {
		field.Filter = []paginate.Operator{paginate.OpEq}
		for _, operator := range operators {
			if !slices.Contains(field.Filter, operator) {
				field.Filter = append(field.Filter, operator)
			}
		}
	})
}

// NotNull marks a column without NULL values that GORM does not know as NOT NULL; see
// paginate.Field.NotNull.
func (f *FieldBuilder) NotNull() *FieldBuilder {
	return f.set(func(field *paginate.Field) { field.NotNull = true })
}

// Type sets the field's type instead of the one the Go type of the model field gives.
func (f *FieldBuilder) Type(t paginate.Type) *FieldBuilder {
	return f.set(func(field *paginate.Field) { field.Type = t })
}

// DefaultSort appends a key of the default sort; the first call is the main key.
func (b *Builder) DefaultSort(dtoField any, order paginate.Order) *Builder {
	if name := b.JSONName(dtoField); name != "" {
		b.defaultSort = append(b.defaultSort, paginate.Sort{Field: name, Order: order})
	}
	return b
}

// TieBreak sets the tie-breaker column explicitly, for example a unique slug instead of the
// primary key.
func (b *Builder) TieBreak(modelField any) *Builder {
	if column, _ := b.column(modelField); column != "" {
		b.tieBreak = column
	}
	return b
}

// WithoutTieBreak turns the tie-breaker off, for example for queries with aggregates.
func (b *Builder) WithoutTieBreak() *Builder {
	b.tieBreak = ""
	return b
}

// Column returns "table.column" of a model field, or "" (and an error from Config) when the
// pointer is not a column of the model: not one of its fields, a relation, or a field GORM
// ignores. Fields of embedded structs (anonymous or tagged gorm:"embedded") are columns of the
// model.
func (b *Builder) Column(modelField any) string {
	column, _ := b.column(modelField)
	return column
}

// JSONName returns the JSON name of a DTO field the way encoding/json names it: the json tag, or
// the field name without one. Fields of nested structs get a dotted path ("author.name"), fields
// of embedded structs are promoted. It returns "" (and an error from Config) when the pointer is
// not a field of the DTO or the field is not serialized (json:"-").
func (b *Builder) JSONName(dtoField any) string {
	path, err := fieldPath(b.dtoPtr, dtoField)
	if err == nil {
		var name string
		if name, err = jsonPath(path); err == nil {
			return name
		}
	}
	b.errs = append(b.errs, fmt.Errorf("builder: dto %T: %w", b.dtoPtr, err))
	return ""
}

// Config returns the fields, the default sort and the tie-breaker, and every pointer that did not
// resolve, joined into one error. Set the rest (Format, limits) on the result, then Compile it.
func (b *Builder) Config() (paginate.Config, error) {
	cfg := paginate.Config{
		Fields:      append([]paginate.Field(nil), b.fields...),
		DefaultSort: append([]paginate.Sort(nil), b.defaultSort...),
		TieBreak:    b.tieBreak,
	}
	return cfg, errors.Join(b.errs...)
}

// column resolves a model field to "table.column" and its Go type.
func (b *Builder) column(modelField any) (string, reflect.Type) {
	if b.db == nil {
		return "", nil
	}
	path, err := fieldPath(b.modelPtr, modelField)
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("builder: model %T: %w", b.modelPtr, err))
		return "", nil
	}
	column, err := columnOf(b.db, b.table, b.modelSchema, path)
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("builder: model %T: %w", b.modelPtr, err))
		return "", nil
	}
	return column, path[len(path)-1].Type
}

// primaryKeyColumn is "table.pk" for a single-column primary key; composite or missing keys give
// no tie-breaker.
func primaryKeyColumn(table string, parsed *schema.Schema) string {
	if parsed == nil || parsed.PrioritizedPrimaryField == nil || len(parsed.PrimaryFields) != 1 {
		return ""
	}
	if parsed.PrioritizedPrimaryField.DBName == "" {
		return ""
	}
	return table + "." + parsed.PrioritizedPrimaryField.DBName
}

// columnOf resolves a field path of the model to "table.column" through the GORM schema.
func columnOf(db *gorm.DB, table string, parsed *schema.Schema, path []reflect.StructField) (string, error) {
	names := make([]string, len(path))
	for i, field := range path {
		names[i] = field.Name
	}
	bindName := strings.Join(names, ".")
	if parsed == nil {
		// The model did not parse (Config says why): only the naming strategy is left.
		return table + "." + db.NamingStrategy.ColumnName(table, names[len(names)-1]), nil
	}
	for _, parent := range path[:len(path)-1] {
		if !parent.Anonymous && !isGormEmbedded(parent) {
			return "", fmt.Errorf("field %s is inside %s, which is neither embedded nor tagged gorm:\"embedded\"", bindName, parent.Name)
		}
	}
	field := parsed.FieldsByBindName[bindName]
	if field == nil {
		field = parsed.LookUpField(names[len(names)-1])
	}
	if field == nil || field.DBName == "" {
		return "", fmt.Errorf("field %s is not a column", bindName)
	}
	return table + "." + field.DBName, nil
}

func isGormEmbedded(field reflect.StructField) bool {
	_, ok := schema.ParseTagSetting(field.Tag.Get("gorm"), ";")["EMBEDDED"]
	return ok
}

// fieldPath finds the struct field fieldPtr points to and returns the fields from the struct down
// to it. The address alone is ambiguous: the first field of a nested struct shares its address with
// the struct itself (&model.Base.ID == &model.Base), so the type must match too. Only fields whose
// memory holds the address are walked into.
func fieldPath(structPtr any, fieldPtr any) ([]reflect.StructField, error) {
	sv := reflect.ValueOf(structPtr)
	if sv.Kind() != reflect.Pointer || sv.IsNil() || sv.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("%T is not a pointer to a struct", structPtr)
	}
	fv := reflect.ValueOf(fieldPtr)
	if fv.Kind() != reflect.Pointer || fv.IsNil() {
		return nil, fmt.Errorf("%T is not a pointer to a field", fieldPtr)
	}
	target := fv.Pointer()
	targetType := fv.Type().Elem()

	var walk func(v reflect.Value) []reflect.StructField
	walk = func(v reflect.Value) []reflect.StructField {
		vt := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			sf := vt.Field(i)
			start := f.Addr().Pointer()
			if target < start || target >= start+f.Type().Size() {
				continue
			}
			if f.Type() == targetType {
				return []reflect.StructField{sf}
			}
			if f.Kind() == reflect.Struct {
				if inner := walk(f); inner != nil {
					return append([]reflect.StructField{sf}, inner...)
				}
			}
		}
		// Embedded pointers to structs live elsewhere in memory.
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			sf := vt.Field(i)
			if sf.Anonymous && f.Kind() == reflect.Pointer && !f.IsNil() && f.Elem().Kind() == reflect.Struct {
				if inner := walk(f.Elem()); inner != nil {
					return append([]reflect.StructField{sf}, inner...)
				}
			}
		}
		return nil
	}

	if path := walk(sv.Elem()); path != nil {
		return path, nil
	}
	return nil, fmt.Errorf("pointer %T is not a field of %T", fieldPtr, structPtr)
}

// jsonPath names a DTO field path the way encoding/json does, nested structs joined with dots.
func jsonPath(path []reflect.StructField) (string, error) {
	segments := make([]string, 0, len(path))
	for i, field := range path {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return "", fmt.Errorf("field %s is not serialized (json:\"-\")", field.Name)
		}
		last := i == len(path)-1
		if name == "" && field.Anonymous && !last {
			// An embedded struct without a JSON name is promoted: its fields belong to the parent.
			continue
		}
		if name == "" {
			name = field.Name
		}
		segments = append(segments, name)
	}
	return strings.Join(segments, "."), nil
}
