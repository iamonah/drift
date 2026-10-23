package util

import (
	"errors"
	"net/http"
	"path/filepath"
	"runtime"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func NewFieldError(field, message string) FieldError {
	return FieldError{
		Field:   field,
		Message: message,
	}
}

type FieldErrors []*FieldError

func NewFieldErrors() FieldErrors {
	return FieldErrors{}
}

func (fe *FieldErrors) Add(field, message string) {
	fev := NewFieldError(field, message)
	*fe = append(*fe, &fev)
}

func (fe FieldErrors) ToError() error {
	if len(fe) == 0 {
		return nil
	}
	return fe
}

func (v FieldErrors) Error() string {
	return "validation error"
}

type AppError struct {
	Err     string      `json:"error"`
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Details FieldErrors `json:"details,omitempty"`

	// Diagnostic fields are for logs only and must never reach clients.
	FileName string `json:"-"`
	FuncName string `json:"-"`
	Line     int    `json:"-"`

	cause error
}

// NewError converts err into a client-safe AppError. The original error is
// retained as the unwrapped cause. For 5xx responses the client message is the
// generic status text and the caller's source is recorded for logging.
func NewError(errCode int, err error) *AppError {
	appErr := &AppError{
		Code:    errCode,
		Err:     http.StatusText(errCode),
		Message: err.Error(),
		cause:   err,
	}

	var fieldErr FieldErrors
	if errors.As(err, &fieldErr) {
		appErr.Details = fieldErr
	}

	// Internal failures keep their cause for logs and errors.Is/As, but the
	// client only sees the generic status text.
	if errCode >= http.StatusInternalServerError {
		appErr.Message = http.StatusText(errCode)

		// Only internal failures are logged with their source. Client errors
		// are expected outcomes and do not need diagnostic locations for now.
		if pc, file, line, ok := runtime.Caller(1); ok {
			appErr.FileName = filepath.Base(file)
			appErr.Line = line
			if fn := runtime.FuncForPC(pc); fn != nil {
				appErr.FuncName = fn.Name()
			}
		}
	}

	return appErr
}

func (e *AppError) Error() string {
	return e.Message
}

// Unwrap preserves the original error chain for errors.Is and errors.As.
func (e *AppError) Unwrap() error {
	return e.cause
}
