package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	midd "github.com/iamonah/drift/cmd/api/mid"
	"github.com/iamonah/drift/internal/util"
)

func (a *App) mux() http.Handler {
	r := mux.NewRouter()
	logged := midd.Logger(&a.log)
	r.HandleFunc("/health", a.handler(a.HealthCheckHandler)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/auth/signup", a.handler(a.SignUp)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/auth/signin", a.handler(a.SignIn)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/auth/refresh", a.handler(a.RefreshToken)).Methods(http.MethodPost)

	return midd.Chain(r.ServeHTTP, midd.RecoverPanic(&a.log), logged, midd.EnableCors(a.cfg))
}

func (a *App) HealthCheckHandler(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
	return nil
}

func (a *App) handler(f func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			var appErr *util.AppError
			if errors.As(err, &appErr) {
				_ = util.WriteJSON(w, appErr.Code, appErr)
				return
			}

			a.log.Error().
				Err(err).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Msg("unhandled request error")

			if writeErr := util.WriteJSON(w, http.StatusInternalServerError,
				util.NewError(http.StatusInternalServerError, errors.New(http.StatusText(http.StatusInternalServerError)))); writeErr != nil {
				a.log.Error().Err(writeErr).Msg("failed to write error response")
			}
		}
	}
}
