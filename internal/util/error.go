package util

import (
	"errors"
	"net/http"
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

func (fe FieldErrors) Add(field, message string) {
	fev := NewFieldError(field, message)
	fe = append(fe, &fev)
}

func (v FieldErrors) Error() string {
	return "validation error"
}

type AppError struct {
	Err     string      `json:"error"`
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Details FieldErrors `json:"details,omitempty"`
}

func NewError(errCode int, err error) *AppError {
	var fliedErr FieldErrors

	if errors.As(err, &fliedErr) {
		return &AppError{
			Code:    errCode,
			Err:     http.StatusText(errCode),
			Message: err.Error(),
			Details: fliedErr,
		}
	}
	return &AppError{
		Code:    errCode,
		Err:     http.StatusText(errCode),
		Message: err.Error(),
	}
}

func (e *AppError) Error() string {
	return e.Message
}
