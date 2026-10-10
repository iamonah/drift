package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatePasswordMinLength(t *testing.T) {
	t.Parallel()

	request := struct {
		Password string `validate:"required,min=6"`
	}{Password: "S3!"}

	err := NewValidate(request)
	require.Error(t, err)

	var fieldErrors FieldErrors
	require.ErrorAs(t, err, &fieldErrors)
	require.Equal(t, "password", fieldErrors[0].Field)
	require.Equal(t, "field must be at least 6 characters long", fieldErrors[0].Message)
}
