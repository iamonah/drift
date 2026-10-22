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
		if err := f(w, r); err != nil {
			var appErr *util.AppError
			if errors.As(err, &appErr) {
				if err = util.WriteJSON(w, appErr.Code, appErr); err != nil {
					log.Error().Err(err).Str("method", r.Method).Str("path", r.URL.Path).
						Msg("failed to write error response")
				}
				return
			}

			log.Error().Err(err).Str("method", r.Method).Str("path", r.URL.Path).Msg("unhandled request error")

			if writeErr := util.WriteJSON(w, http.StatusInternalServerError,
				util.NewError(http.StatusInternalServerError, errors.New(http.StatusText(http.StatusInternalServerError)))); writeErr != nil {
				log.Error().Err(writeErr).Msg("failed to write error response")
			}
		}
	}
}
