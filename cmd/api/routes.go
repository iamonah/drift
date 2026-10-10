package api

import (
	"net/http"

	"github.com/gorilla/mux"
	midd "github.com/iamonah/drift/cmd/api/mid"
)

func (a *App) mux() http.Handler {
	r := mux.NewRouter()
	logged := midd.Logger(&a.log)
	r.HandleFunc("/health", a.HealthCheckHandler).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/auth/register", a.CreateUser).Methods(http.MethodPost)

	return midd.Chain(r.ServeHTTP, midd.RecoverPanic(&a.log), logged, midd.EnableCors(a.cfg))
}

func (a *App) HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
