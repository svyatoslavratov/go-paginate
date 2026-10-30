package paginate

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidRequest marks errors caused by the request: unknown fields, operators that are not
// allowed, malformed or oversized values, values of the wrong type. Answer them with 400.
var ErrInvalidRequest = errors.New("paginate: invalid request")

// ErrInvalidConfig marks errors of a Config: the server's fault, usually a 500.
var ErrInvalidConfig = errors.New("paginate: invalid config")

// Error is an error with a message safe to show to the client; errors.Is matches it with
// ErrInvalidRequest or ErrInvalidConfig.
type Error struct {
	Message string
	Err     error
}

func (e *Error) Error() string {
	return e.Err.Error() + ": " + e.Message
}

// Unwrap lets errors.Is and errors.As see the wrapped sentinel.
func (e *Error) Unwrap() error {
	return e.Err
}

func invalidRequest(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...), Err: ErrInvalidRequest}
}

func invalidConfig(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...), Err: ErrInvalidConfig}
}

// sqlStateError is a database error with a SQLSTATE code, as pgx and lib/pq report them.
type sqlStateError interface {
	error
	SQLState() string
}

// databaseError wraps a failed statement. Postgres rejects a filter value that does not fit the
// column type ("abc" for an integer, a malformed uuid or date) with a data exception, SQLSTATE
// class 22. With filters in the request that is the client's mistake, so it becomes
// ErrInvalidRequest.
func databaseError(stage string, err error, filtered bool) error {
	var stateErr sqlStateError
	if filtered && errors.As(err, &stateErr) && strings.HasPrefix(stateErr.SQLState(), "22") {
		return &Error{Message: "invalid filter value: " + stateErr.Error(), Err: ErrInvalidRequest}
	}
	return fmt.Errorf("paginate: %s: %w", stage, err)
}
