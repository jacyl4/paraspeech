package errs

import "fmt"

type Error struct {
	Code    Code
	Message string
	Cause   error
	Details map[string]any
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d/%s] %s: %v", e.Code, e.Code.String(), e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d/%s] %s", e.Code, e.Code.String(), e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code Code, cause error) *Error {
	return &Error{Code: code, Message: cause.Error(), Cause: cause}
}

func WithDetails(code Code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}
