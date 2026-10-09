package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"uuid"

	"github.com/iamonah/drift/internal/database/driftdb"
	"github.com/iamonah/drift/internal/util"
	"golang.org/x/crypto/bcrypt"
)

func (a *App) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	user := driftdb.User{
		ID:             uuid.New(),
		Email:          req.Email,
		HashedPassword: hashedPassword,
	}

	if err := a.Store.UserStore.InsertUser(r.Context(), user); err != nil {
		util.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to create user: %v", err)})
		return
	}

	a.log.Info().Msgf("User created: %s", user.ID.String())

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": user.ID.String()})
}
