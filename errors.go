package paginate

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrInvalidRequest marks errors caused by the request: unknown fields, operators that are not
// allowed, malformed or oversized values, values of the wrong type, broken cursors. Answer them
// with 400; HTTPStatus does.
var ErrInvalidRequest = errors.New("paginate: invalid request")

// ErrInvalidConfig marks errors of a Config: the server's fault, usually a 500.
var ErrInvalidConfig = errors.New("paginate: invalid config")

// Code says what is wrong, for clients to act on: highlight the filter Error.Field names, show a
// message in their own language.
type Code string

const (
	// CodeInvalidPage means the page is too large for its offset to fit a 64-bit integer.
	CodeInvalidPage Code = "invalid_page"
	// CodeInvalidSort means a sortBy value is not field:ASC or field:DESC.
	CodeInvalidSort Code = "invalid_sort"
	// CodeSortNotAllowed means the field is unknown or not sortable.
	CodeSortNotAllowed Code = "sort_not_allowed"
	// CodeSearchNotAllowed means a searchBy field is unknown or not searchable.
	CodeSearchNotAllowed Code = "search_not_allowed"
	// CodeSearchTooLong means the search term is longer than MaxSearchLength.
	CodeSearchTooLong Code = "search_too_long"
	// CodeUnknownField means a filter names a field the endpoint does not have.
	CodeUnknownField Code = "unknown_field"
	// CodeFilterNotAllowed means the field is not filterable.
	CodeFilterNotAllowed Code = "filter_not_allowed"
	// CodeOperatorNotAllowed means the operator is not allowed on the field.
	CodeOperatorNotAllowed Code = "operator_not_allowed"
	// CodeInvalidValue means a filter value is empty, malformed or does not fit the field's type.
	CodeInvalidValue Code = "invalid_value"
	// CodeTooManyValues means the request has more filter values, or a list more items, than the caps.
	CodeTooManyValues Code = "too_many_values"
	// CodeInvalidExpression means the filter= expression does not parse or is too complex.
	CodeInvalidExpression Code = "invalid_expression"
	// CodeInvalidCursor means the cursor is malformed or belongs to another sort.
	CodeInvalidCursor Code = "invalid_cursor"
	// CodeInvalidConfig means the endpoint's Config is broken; the client can do nothing about it.
	CodeInvalidConfig Code = "invalid_config"
)

// Error is an error with a code and a message safe to show to the client. errors.Is matches it
// with ErrInvalidRequest or ErrInvalidConfig; errors.As gives the code and the field:
//
//	var pe *paginate.Error
//	if errors.As(err, &pe) && pe.Field != "" {
//		// highlight the filter of pe.Field
//	}
//
// Its JSON is {"code": ..., "field": ..., "message": ...}, ready to be the body of a 400.
type Error struct {
	// Code says what is wrong.
	Code Code `json:"code"`
	// Field is the API field the error is about, empty when it is about the request as a whole.
	Field string `json:"field,omitempty"`
	// Message describes the error in terms of the request; it never names columns.
	Message string `json:"message"`
	// Err is ErrInvalidRequest or ErrInvalidConfig.
	Err error `json:"-"`
	// cause is the database error behind a value the database rejected, kept for logs.
	cause error
}

func (e *Error) Error() string {
	return e.Err.Error() + ": " + e.Message
}

// Unwrap lets errors.Is and errors.As see the sentinel and, for values the database rejected, the
// database error.
func (e *Error) Unwrap() []error {
	if e.cause != nil {
		return []error{e.Err, e.cause}
	}
	return []error{e.Err}
}

// HTTPStatus is the status to answer an error of this package with: 400 for ErrInvalidRequest,
// 500 for anything else, 200 for nil.
func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrInvalidRequest):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func invalidRequest(code Code, field, format string, args ...any) error {
	return &Error{Code: code, Field: field, Message: fmt.Sprintf(format, args...), Err: ErrInvalidRequest}
}

func invalidConfig(format string, args ...any) error {
	return &Error{Code: CodeInvalidConfig, Message: fmt.Sprintf(format, args...), Err: ErrInvalidConfig}
}

// maxQuotedValue caps the client values quoted in messages, in bytes.
const maxQuotedValue = 64

// quoteValue quotes a client value for a message, cut to maxQuotedValue bytes on a rune boundary.
func quoteValue(value string) string {
	if len(value) > maxQuotedValue {
		cut := maxQuotedValue
		for cut > 0 && value[cut]&0xC0 == 0x80 {
			cut--
		}
		value = value[:cut] + "…"
	}
	return fmt.Sprintf("%q", value)
}

// sqlStateError is a database error with a SQLSTATE code, as pgx and lib/pq report them.
type sqlStateError interface {
	error
	SQLState() string
}

// databaseError wraps a failed statement. Postgres rejects a value that does not fit the column type
// ("abc" for an integer, a malformed uuid or date) with a data exception, SQLSTATE class 22. With
// client values in the statement (filters or a cursor) that is the client's mistake, so it becomes
// ErrInvalidRequest. Fields with a Type catch such values before the statement runs.
func databaseError(stage string, err error, clientValues bool) error {
	var stateErr sqlStateError
	if clientValues && errors.As(err, &stateErr) && strings.HasPrefix(stateErr.SQLState(), "22") {
		return &Error{
			Code:    CodeInvalidValue,
			Message: "invalid filter value: " + stateErr.Error(),
			Err:     ErrInvalidRequest,
			cause:   err,
		}
	}
	return fmt.Errorf("paginate: %s: %w", stage, err)
}
