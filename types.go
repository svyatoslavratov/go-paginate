package paginate

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Type is the type of a field's values. Prepare checks filter values and cursors against it, so a
// value that does not fit is a 400 naming the field before any query runs. The zero Type checks
// nothing: the database does, and a value it rejects becomes a 400 after the round trip.
type Type struct {
	kind typeKind
	// values are the values of an Enum.
	values []string
	// elem is the element type of an ArrayOf.
	elem *Type
}

type typeKind int

const (
	anyKind typeKind = iota
	textKind
	intKind
	numberKind
	boolKind
	timeKind
	dateKind
	uuidKind
	enumKind
	arrayKind
)

var (
	// Text is any string.
	Text = Type{kind: textKind}
	// Int is a whole number in base 10: 42, -7.
	Int = Type{kind: intKind}
	// Number is a decimal number: 42, -7.5, 1e3.
	Number = Type{kind: numberKind}
	// Bool is true or false (also 1, 0, t, f).
	Bool = Type{kind: boolKind}
	// Time is an RFC 3339 timestamp (2024-05-01T10:00:00Z, 2024-05-01T10:00:00.5+03:00), one
	// without an offset (2024-05-01T10:00:00), or a date (2024-05-01).
	Time = Type{kind: timeKind}
	// Date is a date: 2024-05-01.
	Date = Type{kind: dateKind}
	// UUID is a UUID in the canonical form: 8402e99c-5b52-5201-6079-4615a8161683.
	UUID = Type{kind: uuidKind}
)

// Enum is one of values, compared exactly: an allowlist of what clients may filter by.
func Enum(values ...string) Type {
	return Type{kind: enumKind, values: slices.Clone(values)}
}

// ArrayOf is an array column of elem: $contains checks each of its values against elem.
func ArrayOf(elem Type) Type {
	return Type{kind: arrayKind, elem: &elem}
}

// IsZero reports the zero Type, which checks nothing.
func (t Type) IsZero() bool {
	return t.kind == anyKind
}

// Values returns the values of an Enum, nil for other types.
func (t Type) Values() []string {
	return slices.Clone(t.values)
}

// Elem returns the element type of an ArrayOf, the zero Type for other types.
func (t Type) Elem() Type {
	if t.elem == nil {
		return Type{}
	}
	return *t.elem
}

// IsArray reports an ArrayOf.
func (t Type) IsArray() bool {
	return t.kind == arrayKind
}

// String names the type: "int", "enum(draft|published)", "array of text"; "any" for the zero Type.
func (t Type) String() string {
	switch t.kind {
	case textKind:
		return "text"
	case intKind:
		return "int"
	case numberKind:
		return "number"
	case boolKind:
		return "bool"
	case timeKind:
		return "time"
	case dateKind:
		return "date"
	case uuidKind:
		return "uuid"
	case enumKind:
		return "enum(" + strings.Join(t.values, "|") + ")"
	case arrayKind:
		return "array of " + t.Elem().String()
	}
	return "any"
}

// expects describes the values of the type for an error message, "" for types that take any value.
func (t Type) expects() string {
	switch t.kind {
	case intKind:
		return "an integer"
	case numberKind:
		return "a number"
	case boolKind:
		return "true or false"
	case timeKind:
		return "a timestamp (RFC 3339) or a date (YYYY-MM-DD)"
	case dateKind:
		return "a date (YYYY-MM-DD)"
	case uuidKind:
		return "a UUID"
	case enumKind:
		return "one of " + strings.Join(t.values, ", ")
	}
	return ""
}

// accepts reports whether value fits the type. Arrays accept anything: $contains checks the
// element type instead.
func (t Type) accepts(value string) bool {
	switch t.kind {
	case intKind:
		_, err := strconv.ParseInt(value, 10, 64)
		return err == nil
	case numberKind:
		return isDecimal(value)
	case boolKind:
		_, err := strconv.ParseBool(value)
		return err == nil
	case timeKind:
		return parsesAs(value, timeLayouts)
	case dateKind:
		return parsesAs(value, []string{time.DateOnly})
	case uuidKind:
		return isUUID(value)
	case enumKind:
		return slices.Contains(t.values, value)
	}
	return true
}

// check returns an error naming field when value does not fit the type.
func (t Type) check(field, value string) error {
	if t.accepts(value) {
		return nil
	}
	return invalidRequest(CodeInvalidValue, field, "invalid value %s for %s: must be %s", quoteValue(value), field, t.expects())
}

// timeLayouts are the timestamps Time accepts; a fraction of a second is optional in each.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	time.DateOnly,
}

func parsesAs(value string, layouts []string) bool {
	for _, layout := range layouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

// isDecimal accepts the decimal notation of SQL numbers: digits, a sign, a point, an exponent.
// strconv alone would also take "Inf", "NaN" and hexadecimal floats.
func isDecimal(value string) bool {
	if strings.IndexFunc(value, func(r rune) bool { return !strings.ContainsRune("0123456789+-.eE", r) }) >= 0 {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil || errors.Is(err, strconv.ErrRange)
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// isUUID accepts the canonical form: 8-4-4-4-12 hexadecimal digits.
func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexDigit(c) {
				return false
			}
		}
	}
	return true
}
