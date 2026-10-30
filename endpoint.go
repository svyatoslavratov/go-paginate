package paginate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Endpoint is a compiled Config: checked once, with its lookups built, ready for any number of
// concurrent requests. Build one per list endpoint at startup:
//
//	var usersEndpoint = paginate.MustCompile(paginate.Config{...})
type Endpoint struct {
	cfg Config
	// fields are the fields by API name.
	fields map[string]*Field
	// searchable are the searchable fields in config order.
	searchable []*Field
	// defaultSort is the sort of a request without sortBy.
	defaultSort []sortKey
	// tieBreakField is the API name of the field whose column is the tie-breaker, if any.
	tieBreakField string
}

// sortKey is a resolved sort key: a field and its order.
type sortKey struct {
	field *Field
	order Order
}

// Compile checks cfg and builds an Endpoint, reporting every problem at once: fields without a
// name or a column, repeated names, unknown operators, operators that do not fit the field's type,
// a default sort on unknown fields, cursor pagination without a tie-breaker, unknown modes and
// formats, a default limit over the maximum. Errors wrap ErrInvalidConfig.
func Compile(cfg Config) (*Endpoint, error) {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if cfg.DefaultLimit != 0 && cfg.MaxLimit != 0 && cfg.DefaultLimit > cfg.MaxLimit {
		add("defaultLimit %d exceeds maxLimit %d", cfg.DefaultLimit, cfg.MaxLimit)
	}
	applyDefaults(&cfg)

	e := &Endpoint{fields: make(map[string]*Field, len(cfg.Fields))}
	cfg.Fields = slices.Clone(cfg.Fields)
	for i := range cfg.Fields {
		field := &cfg.Fields[i]
		field.Filter = slices.Clone(field.Filter)
		if !field.Type.IsZero() && !field.Type.IsArray() && containsAll(field.Filter) {
			field.Filter = slices.DeleteFunc(field.Filter, func(o Operator) bool { return o == OpContains })
		}
		// $eq is allowed on every filterable field; listing it, once, lets Fields tell the truth.
		if field.Filter != nil {
			operators := []Operator{OpEq}
			for _, operator := range field.Filter {
				if !slices.Contains(operators, operator) {
					operators = append(operators, operator)
				}
			}
			field.Filter = operators
		}
		switch {
		case field.Name == "":
			add("field %d: name is required", i)
			continue
		case field.Column == "":
			add("field %s: column is required", field.Name)
		case e.fields[field.Name] != nil:
			add("field %s: defined twice", field.Name)
			continue
		}
		e.fields[field.Name] = field
		for _, err := range checkField(field) {
			add("field %s: %v", field.Name, err)
		}
		if field.Searchable {
			e.searchable = append(e.searchable, field)
		}
		if e.tieBreakField == "" && cfg.TieBreak != "" && field.Column == cfg.TieBreak {
			e.tieBreakField = field.Name
		}
	}

	for _, key := range cfg.DefaultSort {
		field := e.fields[key.Field]
		order, ok := parseOrder(string(key.Order))
		switch {
		case field == nil:
			add("default sort: unknown field %s", key.Field)
		case !ok:
			add("default sort %s: order must be %s or %s, got %q", key.Field, Asc, Desc, key.Order)
		case !slices.ContainsFunc(e.defaultSort, func(k sortKey) bool { return k.field == field }):
			e.defaultSort = append(e.defaultSort, sortKey{field: field, order: order})
		}
	}
	if len(e.defaultSort) == 0 {
		for i := range cfg.Fields {
			if field := &cfg.Fields[i]; field.Sortable && e.fields[field.Name] == field {
				e.defaultSort = []sortKey{{field: field, order: Asc}}
				break
			}
		}
	}
	if len(e.defaultSort) == 0 && cfg.TieBreak == "" {
		add("no sort: set DefaultSort, make a field sortable, or set TieBreak")
	}

	switch cfg.Pagination {
	case OffsetPagination:
	case CursorPagination:
		if cfg.TieBreak == "" {
			add("cursor pagination needs a TieBreak")
		}
	default:
		add("unknown pagination %d", cfg.Pagination)
	}
	if cfg.Count < CountExact || cfg.Count > CountNone {
		add("unknown count %d", cfg.Count)
	}
	if cfg.NullSort < 0 || cfg.NullSort > NullsLast {
		add("unknown null sort %d", cfg.NullSort)
	}
	if cfg.Format != FormatDefault && cfg.Format != FormatNestJS {
		add("unknown format %q", cfg.Format)
	}

	if len(errs) != 0 {
		return nil, &Error{Code: CodeInvalidConfig, Message: errors.Join(errs...).Error(), Err: ErrInvalidConfig}
	}
	e.cfg = cfg
	return e, nil
}

// MustCompile is Compile that panics on a broken config, for endpoints declared as package
// variables: a broken config then stops the service at startup.
func MustCompile(cfg Config) *Endpoint {
	e, err := Compile(cfg)
	if err != nil {
		panic(err)
	}
	return e
}

// Fields returns the fields of the endpoint, for building a filter UI or API documentation. The
// operators of a filterable field include $eq and leave out what does not fit its type.
func (e *Endpoint) Fields() []Field {
	fields := slices.Clone(e.cfg.Fields)
	for i := range fields {
		fields[i].Filter = slices.Clone(fields[i].Filter)
	}
	return fields
}

// Config returns the config the endpoint was compiled from, with the defaults filled in.
func (e *Endpoint) Config() Config {
	cfg := e.cfg
	cfg.Fields = e.Fields()
	cfg.DefaultSort = slices.Clone(cfg.DefaultSort)
	return cfg
}

// applyDefaults fills the zero limits.
func applyDefaults(cfg *Config) {
	if cfg.DefaultLimit == 0 {
		cfg.DefaultLimit = DefaultLimit
	}
	if cfg.MaxLimit == 0 {
		cfg.MaxLimit = DefaultMaxLimit
	}
	cfg.DefaultLimit = min(cfg.DefaultLimit, cfg.MaxLimit)
	if cfg.MaxSearchLength == 0 {
		cfg.MaxSearchLength = DefaultMaxSearchLength
	}
	if cfg.MaxFilterConditions == 0 {
		cfg.MaxFilterConditions = DefaultMaxFilterConditions
	}
	if cfg.MaxFilterValues == 0 {
		cfg.MaxFilterValues = DefaultMaxFilterValues
	}
	if cfg.MaxFilterExpressionComplexity == 0 {
		cfg.MaxFilterExpressionComplexity = DefaultMaxFilterExpressionComplexity
	}
}

// checkField reports the problems of a field's operators and type.
func checkField(field *Field) []error {
	var errs []error
	for _, operator := range field.Filter {
		switch {
		case isComparator(string(operator)):
			errs = append(errs, fmt.Errorf("%s is not an operator: values choose it themselves", operator))
		case !isOperator(string(operator)) && !isSuffix(string(operator)):
			errs = append(errs, fmt.Errorf("unknown operator %s", operator))
		case operator == OpContains && !field.Type.IsZero() && !field.Type.IsArray():
			errs = append(errs, fmt.Errorf("$contains needs an array type, the field is %s", field.Type))
		}
	}
	if field.Type.kind == enumKind && len(field.Type.values) == 0 {
		errs = append(errs, errors.New("an enum needs values"))
	}
	return errs
}

// containsAll reports a list with every operator: AllOperators.
func containsAll(operators []Operator) bool {
	for _, operator := range AllOperators {
		if !slices.Contains(operators, operator) {
			return false
		}
	}
	return true
}

// columnName is the last part of a column reference, which rows name the column by: released_at
// for products.released_at. It is empty for expressions.
func columnName(column string) string {
	if strings.ContainsAny(column, "() ,:") {
		return ""
	}
	return strings.Trim(column[strings.LastIndexByte(column, '.')+1:], `"`)
}
