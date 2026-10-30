package gormx

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/svyatoslavratov/go-paginate"
)

// ConfigBuilder builds a paginate.Config from pointers to model and DTO fields instead of
// hand-written names: &model.Title becomes "templates.title" through the GORM schema, &dto.Title
// becomes its JSON name. A renamed field then breaks the build, not a request. A pointer the
// builder cannot resolve is skipped and reported by Build.
type ConfigBuilder struct {
	db             *gorm.DB
	tableName      string
	modelPtr       any
	dtoPtr         any
	modelSchema    *schema.Schema
	sortable       []string
	searchable     []string
	mapInputFields map[string]string
	filterable     paginate.FilterableColumns
	defaultSortBy  paginate.SortBy
	// defaultSortKeys keeps the order of AddDefaultSort calls: the first key is the main one.
	defaultSortKeys []string
	// tieBreak is the primary key column of the model (empty for composite or unknown keys).
	tieBreak string
	errs     []error
}

// NewConfigBuilder starts a config for tableName, the table or alias the query selects from.
// modelPtr and dtoPtr must be pointers to structs; the fields passed to the methods must belong to
// them. db supplies the naming strategy; it is never queried.
func NewConfigBuilder(db *gorm.DB, tableName string, modelPtr any, dtoPtr any) *ConfigBuilder {
	b := &ConfigBuilder{
		db:              db,
		tableName:       tableName,
		modelPtr:        modelPtr,
		dtoPtr:          dtoPtr,
		sortable:        make([]string, 0),
		searchable:      make([]string, 0),
		mapInputFields:  make(map[string]string),
		filterable:      make(paginate.FilterableColumns),
		defaultSortBy:   make(paginate.SortBy),
		defaultSortKeys: make([]string, 0),
	}
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
	b.tieBreak = primaryKeyColumn(tableName, b.modelSchema)
	return b
}

// primaryKeyColumn is "table.pk" for a single-column primary key; composite or missing keys
// give no tie-breaker.
func primaryKeyColumn(tableName string, parsed *schema.Schema) string {
	if parsed == nil || parsed.PrioritizedPrimaryField == nil || len(parsed.PrimaryFields) != 1 {
		return ""
	}
	if parsed.PrioritizedPrimaryField.DBName == "" {
		return ""
	}
	return fmt.Sprintf("%s.%s", tableName, parsed.PrioritizedPrimaryField.DBName)
}

// WithTieBreak sets the tie-breaker column explicitly (for example a unique slug instead of id).
func (b *ConfigBuilder) WithTieBreak(fieldPtr any) *ConfigBuilder {
	if column := b.Column(fieldPtr); column != "" {
		b.tieBreak = column
	}
	return b
}

// WithoutTieBreak turns the tie-breaker off (for example for queries with aggregates).
func (b *ConfigBuilder) WithoutTieBreak() *ConfigBuilder {
	b.tieBreak = ""
	return b
}

// Column returns "table.column" of a model field, or "" (and an error from Build) when the pointer is
// not a column of the model: not one of its fields, a relation, or a field GORM ignores. Fields of
// embedded structs (anonymous or tagged gorm:"embedded") are columns of the model.
func (b *ConfigBuilder) Column(fieldPtr any) string {
	if b == nil {
		return ""
	}
	if b.db == nil {
		return ""
	}
	path, err := fieldPath(b.modelPtr, fieldPtr)
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("builder: model %T: %w", b.modelPtr, err))
		return ""
	}
	column, err := b.columnName(path)
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("builder: model %T: %w", b.modelPtr, err))
		return ""
	}
	return fmt.Sprintf("%s.%s", b.tableName, column)
}

// JSONName returns the JSON name of a DTO field the way encoding/json names it: the json tag, or
// the field name without one. Fields of nested structs get a dotted path ("author.name"), fields
// of embedded structs are promoted. It returns "" (and an error from Build) when the pointer is not
// a field of the DTO or the field is not serialized (json:"-").
func (b *ConfigBuilder) JSONName(fieldPtr any) string {
	if b == nil {
		return ""
	}
	path, err := fieldPath(b.dtoPtr, fieldPtr)
	if err == nil {
		var name string
		if name, err = jsonPath(path); err == nil {
			return name
		}
	}
	b.errs = append(b.errs, fmt.Errorf("builder: dto %T: %w", b.dtoPtr, err))
	return ""
}

// AddSortable allows sorting by the model fields.
func (b *ConfigBuilder) AddSortable(fieldPtrs ...any) *ConfigBuilder {
	for _, fieldPtr := range fieldPtrs {
		column := b.Column(fieldPtr)
		if column == "" {
			continue
		}
		b.sortable = append(b.sortable, column)
	}
	return b
}

// AddSearchable makes the model fields part of the search.
func (b *ConfigBuilder) AddSearchable(fieldPtrs ...any) *ConfigBuilder {
	for _, fieldPtr := range fieldPtrs {
		column := b.Column(fieldPtr)
		if column == "" {
			continue
		}
		b.searchable = append(b.searchable, column)
	}
	return b
}

// AddDefaultSort appends a default sort key; the first call is the main key.
func (b *ConfigBuilder) AddDefaultSort(fieldPtr any, order paginate.Order) *ConfigBuilder {
	column := b.Column(fieldPtr)
	if column == "" {
		return b
	}
	if _, exists := b.defaultSortBy[column]; !exists {
		b.defaultSortKeys = append(b.defaultSortKeys, column)
	}
	b.defaultSortBy[column] = order
	return b
}

// AddFilterable allows filtering the model field with the operators; $eq is always allowed, so no
// operators means $eq only. paginate.AllOperators allows every operator.
func (b *ConfigBuilder) AddFilterable(fieldPtr any, operators ...paginate.Operator) *ConfigBuilder {
	column := b.Column(fieldPtr)
	if column == "" {
		return b
	}
	b.filterable[column] = append([]paginate.Operator{}, operators...)
	return b
}

// MapField maps the JSON name of a DTO field (what clients send) to a model column.
func (b *ConfigBuilder) MapField(dtoFieldPtr any, modelFieldPtr any) *ConfigBuilder {
	key := b.JSONName(dtoFieldPtr)
	column := b.Column(modelFieldPtr)
	if key == "" || column == "" {
		return b
	}
	b.mapInputFields[key] = column
	return b
}

// Build returns the config and the pointers it could not resolve, joined into one error. The
// config is usable either way: unresolved fields are left out of it.
func (b *ConfigBuilder) Build() (paginate.Config, error) {
	cfg := paginate.NewConfig(b.tableName, b.sortable, b.mapInputFields)
	cfg.DefaultSortBy = b.defaultSortBy
	cfg.DefaultSortKeys = b.defaultSortKeys
	cfg.TieBreakColumn = b.tieBreak
	cfg.SearchableColumns = b.searchable
	cfg.FilterableColumns = b.filterable
	return *cfg, errors.Join(b.errs...)
}

// columnName resolves a field path of the model to its column through the GORM schema.
func (b *ConfigBuilder) columnName(path []reflect.StructField) (string, error) {
	names := make([]string, len(path))
	for i, field := range path {
		names[i] = field.Name
	}
	bindName := strings.Join(names, ".")
	if b.modelSchema == nil {
		// The model did not parse (Err says why): only the naming strategy is left.
		return b.db.NamingStrategy.ColumnName(b.tableName, names[len(names)-1]), nil
	}
	for _, parent := range path[:len(path)-1] {
		if !parent.Anonymous && !isGormEmbedded(parent) {
			return "", fmt.Errorf("field %s is inside %s, which is neither embedded nor tagged gorm:\"embedded\"", bindName, parent.Name)
		}
	}
	field := b.modelSchema.FieldsByBindName[bindName]
	if field == nil {
		field = b.modelSchema.LookUpField(names[len(names)-1])
	}
	if field == nil || field.DBName == "" {
		return "", fmt.Errorf("field %s is not a column", bindName)
	}
	return field.DBName, nil
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
