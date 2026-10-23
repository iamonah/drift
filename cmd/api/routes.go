package main

import (
	"errors"
	"net/http"

	midd "github.com/iamonah/drift/cmd/api/mid"
	"github.com/iamonah/drift/internal/util"
)

func (a *App) mux() http.Handler {
	logged := midd.Logger(&a.log)
	authenticated := midd.AuthBearerToken(a.jwtMaker)

	a.Handle(http.MethodGet, "/health", a.handler(a.HealthCheckHandler))
	a.Handle(http.MethodPost, "/api/v1/auth/signup", a.SignUp())
	a.Handle(http.MethodPost, "/api/v1/auth/signin", a.SignIn())
	a.Handle(http.MethodPost, "/api/v1/auth/refresh", a.RefreshToken())

	a.Handle(http.MethodPost, "/api/v1/reports", a.CreateReport(), authenticated)
	a.Handle(http.MethodGet, "/api/v1/reports/{report_id}", a.GetReportByUserID(), authenticated)
	a.Handle(http.MethodGet, "/api/v1/reports/{report_id}/download", a.DownloadReport(), authenticated)
	return midd.Chain(a.router.ServeHTTP, midd.RecoverPanic(), logged, midd.EnableCors(a.cfg))
}

func (a *App) Handle(method, pattern string, handler http.HandlerFunc, middle ...midd.Middleware) {
	h := midd.Chain(handler, middle...)
	a.router.HandleFunc(pattern, h).Methods(method)
}

func (a *App) HealthCheckHandler(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
	return nil
}

func (a *App) handler(f func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := a.logger(r)
		err := f(w, r)
		if err == nil {
			return
		}

		var appErr *util.AppError
		if !errors.As(err, &appErr) {
			// Last-resort conversion for errors that did not go through a
			// handler. The cause is kept for logging and errors.Is/As.
			appErr = util.NewError(http.StatusInternalServerError, err)
		}

		// Internal failures are logged once at the HTTP boundary. Expected
		// client errors are not logged here.
		if appErr.Code >= http.StatusInternalServerError {
			cause := appErr.Unwrap()
			if cause == nil {
				cause = appErr
			}

			log.Error().
				Err(cause).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status_code", appErr.Code).
				Str("error_source", appErr.FileName).
				Int("error_line", appErr.Line).
				Str("error_func", appErr.FuncName).
				Msg("request failed")
		}

		if writeErr := util.WriteJSON(w, appErr.Code, appErr); writeErr != nil {
			log.Error().Err(writeErr).Str("method", r.Method).Str("path", r.URL.Path).
				Msg("failed to write error response")
		}
	}
}
