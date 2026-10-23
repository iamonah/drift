package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iamonah/drift/internal/util"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func requestWithLogs(logs *bytes.Buffer) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
	logger := zerolog.New(logs)

	return r.WithContext(logger.WithContext(r.Context()))
}

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return body
}

func TestHandlerInternalErrorHidesCauseAndLogsOnce(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	app := &App{}
	handler := app.handler(func(http.ResponseWriter, *http.Request) error {
		return util.NewError(http.StatusInternalServerError,
			fmt.Errorf("create report: %w", errors.New("database password=secret")))
	})

	rec := httptest.NewRecorder()
	handler(rec, requestWithLogs(&logs))

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	body := decodeErrorBody(t, rec)
	require.Equal(t, http.StatusText(http.StatusInternalServerError), body["message"])
	require.NotContains(t, rec.Body.String(), "database password=secret")
	require.NotContains(t, rec.Body.String(), "create report")
	require.NotContains(t, body, "file")
	require.NotContains(t, body, "func")

	require.Equal(t, 1, strings.Count(logs.String(), "request failed"))
	require.Contains(t, logs.String(), "database password=secret")
	require.Contains(t, logs.String(), "error_source")
	require.Contains(t, logs.String(), "error_line")
	require.Contains(t, logs.String(), "error_func")
}

func TestHandlerClientErrorIsNotLogged(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	app := &App{}
	handler := app.handler(func(http.ResponseWriter, *http.Request) error {
		return util.NewError(http.StatusBadRequest, errors.New("invalid request payload"))
	})

	rec := httptest.NewRecorder()
	handler(rec, requestWithLogs(&logs))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid request payload", decodeErrorBody(t, rec)["message"])
	require.Empty(t, logs.String())
}

func TestHandlerUnhandledErrorIsWrappedAndLogged(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	app := &App{}
	handler := app.handler(func(http.ResponseWriter, *http.Request) error {
		return errors.New("unexpected failure")
	})

	rec := httptest.NewRecorder()
	handler(rec, requestWithLogs(&logs))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, http.StatusText(http.StatusInternalServerError), decodeErrorBody(t, rec)["message"])
	require.NotContains(t, rec.Body.String(), "unexpected failure")

	require.Equal(t, 1, strings.Count(logs.String(), "request failed"))
	require.Contains(t, logs.String(), "unexpected failure")
	require.Contains(t, logs.String(), "routes.go")
}

func TestHandlerPreservesWrappedAppErrorStatus(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	app := &App{}
	handler := app.handler(func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("outer: %w",
			util.NewError(http.StatusConflict, errors.New("user already exists")))
	})

	rec := httptest.NewRecorder()
	handler(rec, requestWithLogs(&logs))

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Equal(t, "user already exists", decodeErrorBody(t, rec)["message"])
	require.Empty(t, logs.String())
}
