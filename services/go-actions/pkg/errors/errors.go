package errors

import "fmt"

// Error types for the application
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// New creates a new error
func New(code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an error with code and message
func Wrap(err error, code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// Common error codes
const (
	ErrCodeKeycloakAuth     = "KEYCLOAK_AUTH_ERROR"
	ErrCodeKeycloakAPI      = "KEYCLOAK_API_ERROR"
	ErrCodeHasuraAPI        = "HASURA_API_ERROR"
	ErrCodeUserNotFound     = "USER_NOT_FOUND"
	ErrCodeUserCreateFailed = "USER_CREATE_FAILED"
	ErrCodeInvalidRequest   = "INVALID_REQUEST"
	ErrCodeUnauthorized     = "UNAUTHORIZED"
)
