package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

type testCause struct {
	message string
}

func (e *testCause) Error() string {
	return e.message
}

func TestNewErrorKeepsCauseChain(t *testing.T) {
	t.Parallel()

	root := errors.New("connection refused")
	wrapped := fmt.Errorf("repository: %w", fmt.Errorf("load user: %w", root))

	err := NewError(http.StatusInternalServerError, wrapped)

	require.Equal(t, http.StatusInternalServerError, err.Code)
	require.Equal(t, http.StatusText(http.StatusInternalServerError), err.Message)
	require.NotContains(t, err.Message, "connection refused")
	require.ErrorIs(t, err, wrapped)
	require.ErrorIs(t, err, root)
}

func TestNewErrorKeepsCauseForErrorsAs(t *testing.T) {
	t.Parallel()

	cause := &testCause{message: "query failed"}
	err := NewError(http.StatusInternalServerError, fmt.Errorf("load user: %w", cause))

	var target *testCause
	require.ErrorAs(t, err, &target)
	require.Same(t, cause, target)
}

func TestNewErrorKeepsClientMessage(t *testing.T) {
	t.Parallel()

	err := NewError(http.StatusNotFound, errors.New("report not found"))

	require.Equal(t, "report not found", err.Message)
}

func TestNewErrorKeepsValidationDetails(t *testing.T) {
	t.Parallel()

	fields := NewFieldErrors()
	fields.Add("email", "field is required")

	err := NewError(http.StatusBadRequest, fields.ToError())

	require.Equal(t, http.StatusBadRequest, err.Code)
	require.Equal(t, "validation error", err.Message)
	require.Len(t, err.Details, 1)
	require.Equal(t, "email", err.Details[0].Field)

	var got FieldErrors
	require.ErrorAs(t, err, &got)
	require.Len(t, got, 1)
}

func TestNewErrorCapturesCallerSourceForInternalErrors(t *testing.T) {
	t.Parallel()

	_, _, wantLine, _ := runtime.Caller(0)
	err := NewError(http.StatusInternalServerError, errors.New("internal failure"))

	require.Equal(t, "error_test.go", err.FileName)
	require.Equal(t, wantLine+1, err.Line)
	require.Contains(t, err.FuncName, "TestNewErrorCapturesCallerSourceForInternalErrors")
}

func TestNewErrorDoesNotCaptureSourceForClientErrors(t *testing.T) {
	t.Parallel()

	err := NewError(http.StatusBadRequest, errors.New("bad request"))

	require.Empty(t, err.FileName)
	require.Empty(t, err.FuncName)
	require.Zero(t, err.Line)
}

func TestNewErrorHidesDiagnosticsFromJSON(t *testing.T) {
	t.Parallel()

	err := NewError(http.StatusInternalServerError, errors.New("password=secret"))

	data, marshalErr := json.Marshal(err)
	require.NoError(t, marshalErr)

	var body map[string]any
	require.NoError(t, json.Unmarshal(data, &body))

	require.Equal(t, http.StatusText(http.StatusInternalServerError), body["message"])
	require.NotContains(t, body, "file")
	require.NotContains(t, body, "func")
	require.NotContains(t, body, "line")
	require.NotContains(t, body, "cause")
	require.NotContains(t, string(data), "password=secret")
}
