package util

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

var Validate = validator.New(validator.WithRequiredStructEnabled())

func NewValidate(data any) error {
	err := Validate.Struct(data)
	fe := NewFieldErrors()

	if err != nil {
		validationErrors, ok := err.(validator.ValidationErrors)
		if ok {
			for _, verr := range validationErrors {
				switch verr.Tag() {
				case "required":
					fe.Add(strings.ToLower(verr.Field()), "field is required")
				case "min":
					fe.Add(strings.ToLower(verr.Field()), fmt.Sprintf("field must be at least %s characters long", verr.Param()))
				case "email":
					fe.Add(strings.ToLower(verr.Field()), "field must be a valid email address")
				default:
					fe.Add(strings.ToLower(verr.Field()), fmt.Sprintf("field is invalid: %s", verr.Tag()))
				}
			}
		}
	}
	return fe.ToError()
}
