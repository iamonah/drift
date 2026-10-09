package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/iamonah/drift/cmd/api/mid"
)

func (a *App) mux() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/health", a.HealthCheckHandler).Methods(http.MethodGet)
	r.HandleFunc("/users", a.CreateUser).Methods(http.MethodPost)

	return midd.Chain(r, midd.RecoverPanic(&a.log), midd.EnableCors(*a.cfg), midd.AuthBearerToken(&a.log, a.jwtMaker))
}

func (a *App) HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
