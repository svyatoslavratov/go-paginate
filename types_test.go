package paginate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTypeAccepts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typ  Type
		good []string
		bad  []string
	}{
		{typ: Text, good: []string{"", "anything", "1"}},
		{typ: Int, good: []string{"0", "-7", "+42", "9223372036854775807"}, bad: []string{"", "1.5", "1e3", "abc", "9223372036854775808", " 1"}},
		{typ: Number, good: []string{"0", "-7.5", "+.5", "5.", "1e3", "1E-3", "1e400"}, bad: []string{"", "NaN", "Inf", "infinity", "0x10", "1_000", "1,5", "e3", "--1"}},
		{typ: Bool, good: []string{"true", "false", "1", "0", "t", "F", "TRUE"}, bad: []string{"", "yes", "2"}},
		{typ: Time, good: []string{"2024-05-01", "2024-05-01T10:00:00Z", "2024-05-01T10:00:00.123+03:00", "2024-05-01T10:00:00", "2024-05-01 10:00:00"}, bad: []string{"", "yesterday", "2024-13-01", "01.05.2024"}},
		{typ: Date, good: []string{"2024-05-01", "2024-02-29"}, bad: []string{"2024-02-30", "2024-05-01T10:00:00Z", "2024-5-1"}},
		{typ: UUID, good: []string{"8402e99c-5b52-5201-6079-4615a8161683", "8402E99C-5B52-5201-6079-4615A8161683"}, bad: []string{"", "8402e99c5b5252016079461 5a8161683", "8402e99c-5b52-5201-6079-4615a816168", "{8402e99c-5b52-5201-6079-4615a8161683}", "8402e99c-5b52-5201-6079-4615a816168g"}},
		{typ: Enum("draft", "published"), good: []string{"draft", "published"}, bad: []string{"", "Draft", "archived"}},
		{typ: Type{}, good: []string{"", "anything"}},
	}
	for _, tc := range tests {
		t.Run(tc.typ.String(), func(t *testing.T) {
			t.Parallel()
			for _, value := range tc.good {
				if !tc.typ.accepts(value) {
					t.Errorf("%q must be accepted", value)
				}
			}
			for _, value := range tc.bad {
				if tc.typ.accepts(value) {
					t.Errorf("%q must be refused", value)
				}
			}
		})
	}
}

func TestTypeNames(t *testing.T) {
	t.Parallel()
	for typ, want := range map[*Type]string{
		{}:                       "any",
		&Text:                    "text",
		ptrType(Enum("a", "b")):  "enum(a|b)",
		ptrType(ArrayOf(Int)):    "array of int",
		ptrType(ArrayOf(Type{})): "array of any",
	} {
		if got := typ.String(); got != want {
			t.Errorf("want %s, got %s", want, got)
		}
	}
	if values := Enum("a", "b").Values(); !reflect.DeepEqual(values, []string{"a", "b"}) {
		t.Fatalf("enum values %v", values)
	}
	if !ArrayOf(Int).IsArray() || ArrayOf(Int).Elem().String() != "int" || !Int.Elem().IsZero() {
		t.Fatal("array types")
	}
}

func ptrType(t Type) *Type {
	return &t
}

type nullable struct {
	Valid bool
	Count int32
}

func TestTypeOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value any
		want  string
	}{
		{int64(1), "int"},
		{uint8(1), "int"},
		{1.5, "number"},
		{"", "text"},
		{true, "bool"},
		{time.Time{}, "time"},
		{new(time.Time), "time"},
		{[16]byte{}, "uuid"},
		{[]string{}, "array of text"},
		{[]int{}, "array of int"},
		{[]byte{}, "any"},
		{sql.NullInt64{}, "int"},
		{sql.Null[time.Time]{}, "time"},
		{nullable{}, "int"},
		{struct{ A, B int }{}, "any"},
		{map[string]int{}, "any"},
	}
	for _, tc := range tests {
		if got := TypeOf(reflect.TypeOf(tc.value)).String(); got != tc.want {
			t.Errorf("%T: want %s, got %s", tc.value, tc.want, got)
		}
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	err := invalidRequest(CodeOperatorNotAllowed, "age", "operator %s is not allowed for %s", OpILike, "age")
	if !errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrInvalidConfig) {
		t.Fatal("an invalid request must match ErrInvalidRequest only")
	}
	if err.Error() != "paginate: invalid request: operator $ilike is not allowed for age" {
		t.Fatalf("message %q", err.Error())
	}
	body, _ := json.Marshal(err)
	if string(body) != `{"code":"operator_not_allowed","field":"age","message":"operator $ilike is not allowed for age"}` {
		t.Fatalf("JSON %s", body)
	}

	for err, want := range map[error]int{
		nil:                         http.StatusOK,
		err:                         http.StatusBadRequest,
		invalidConfig("broken"):     http.StatusInternalServerError,
		errors.New("database down"): http.StatusInternalServerError,
	} {
		if got := HTTPStatus(err); got != want {
			t.Errorf("%v: want %d, got %d", err, want, got)
		}
	}

	long := strings.Repeat("я", 40)
	if got := quoteValue(long); !strings.HasSuffix(got, `…"`) || len(got) > maxQuotedValue+8 || !strings.HasPrefix(got, `"яя`) {
		t.Fatalf("a long value must be cut on a rune boundary: %s", got)
	}
}
