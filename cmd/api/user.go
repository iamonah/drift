package api

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/iamonah/drift/internal/database/driftdb"
	"github.com/iamonah/drift/internal/util"
)

type createUserRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
}

type createUserResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *App) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := util.ReadJSON(r, &req); err != nil {
		a.writeError(w, http.StatusBadRequest, errors.New("invalid request payload"))
		return
	}

	if err := util.NewValidate(req); err != nil {
		a.writeError(w, http.StatusBadRequest, err)
		return
	}

	hashedPassword, err := util.HashPassword(req.Password)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, errors.New(http.StatusText(http.StatusInternalServerError)))
		return
	}

	data := driftdb.User{
		ID:             uuid.New(),
		Email:          req.Email,
		HashedPassword: hashedPassword,
	}
	user, err := a.Store.UserStore.InsertUser(r.Context(), data)
	if err != nil {
		if errors.Is(err, driftdb.ErrUserAlreadyExists) {
			a.writeError(w, http.StatusConflict, errors.New("user already exists"))
			return
		}

		a.writeError(w, http.StatusInternalServerError, errors.New(http.StatusText(http.StatusInternalServerError)))
		return
	}

	a.log.Info().Str("user_id", user.ID.String()).Msg("user created")
	a.writeJSON(w, http.StatusCreated, createUserResponse{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
}

func (a *App) writeError(w http.ResponseWriter, status int, err error) {
	a.writeJSON(w, status, util.NewError(status, err))
}

func (a *App) writeJSON(w http.ResponseWriter, status int, body any) {
	if err := util.WriteJSON(w, status, body); err != nil {
		a.log.Error().Err(err).Msg("failed to write JSON response")
	}
}
