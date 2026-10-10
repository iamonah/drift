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
}

func NewError(errCode int, err error) *AppError {
	var fieldErr FieldErrors

	if errors.As(err, &fieldErr) {
		return &AppError{
			Code:    errCode,
			Err:     http.StatusText(errCode),
			Message: err.Error(),
			Details: fieldErr,
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
