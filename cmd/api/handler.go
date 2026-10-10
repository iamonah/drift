package api

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/database/driftdb"
	"github.com/iamonah/drift/internal/util"
)

type credentialsRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type signInResponse struct {
	User                  userResponse `json:"user"`
	AccessToken           string       `json:"access_token"`
	AccessTokenExpiresAt  time.Time    `json:"access_token_expires_at"`
	RefreshToken          string       `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time    `json:"refresh_token_expires_at"`
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (a *App) SignUp(w http.ResponseWriter, r *http.Request) error {
	var req credentialsRequest
	if err := util.ReadJSON(r, &req); err != nil {
		return util.NewError(http.StatusBadRequest, errors.New("invalid request payload"))
	}

	if err := util.NewValidate(req); err != nil {
		return util.NewError(http.StatusBadRequest, err)
	}

	hashedPassword, err := util.HashPassword(req.Password)
	if err != nil {
		return err
	}

	data := driftdb.User{
		ID:             uuid.New(),
		Email:          req.Email,
		HashedPassword: hashedPassword,
	}
	user, err := a.Store.UserStore.InsertUser(r.Context(), data)
	if err != nil {
		if errors.Is(err, driftdb.ErrUserAlreadyExists) {
			return util.NewError(http.StatusConflict, errors.New("user already exists"))
		}

		return err
	}

	a.log.Info().Str("user_id", user.ID.String()).Msg("user created")
	a.writeJSON(w, http.StatusCreated, userResponse{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
	return nil
}

func (a *App) SignIn(w http.ResponseWriter, r *http.Request) error {
	var req credentialsRequest
	if err := util.ReadJSON(r, &req); err != nil {
		return util.NewError(http.StatusBadRequest, errors.New("invalid request payload"))
	}

	if err := util.NewValidate(req); err != nil {
		return util.NewError(http.StatusBadRequest, err)
	}

	user, err := a.Store.UserStore.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, driftdb.ErrUserDoesNotExist) {
			return util.NewError(http.StatusUnauthorized, errors.New("invalid email or password"))
		}

		return err
	}
	if !util.CheckPasswordHash(req.Password, string(user.HashedPassword)) {
		return util.NewError(http.StatusUnauthorized, errors.New("invalid email or password"))
	}

	response, err := a.createSession(r, user, a.Store)
	if err != nil {
		return err
	}

	a.log.Info().Str("user_id", user.ID.String()).Msg("user signed in")
	a.writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) createSession(r *http.Request, user driftdb.User, store *driftdb.Store) (signInResponse, error) {
	accessDuration, err := config.ParseDuration(a.cfg.JWT.AccessTokenDuration)
	if err != nil {
		return signInResponse{}, err
	}
	refreshDuration, err := config.ParseDuration(a.cfg.JWT.RefreshTokenDuration)
	if err != nil {
		return signInResponse{}, err
	}

	accessToken, accessPayload, err := a.jwtMaker.GenerateToken(util.JWTData{
		UserID:      user.ID,
		Duration:    accessDuration,
		ServiceName: a.cfg.JWT.Issuer,
		Audience:    a.cfg.JWT.Audience,
		TokenType:   util.TokenTypeAccess,
	})
	if err != nil {
		return signInResponse{}, err
	}
	refreshToken, refreshPayload, err := a.jwtMaker.GenerateToken(util.JWTData{
		UserID:      user.ID,
		Duration:    refreshDuration,
		ServiceName: a.cfg.JWT.Issuer,
		Audience:    a.cfg.JWT.Audience,
		TokenType:   util.TokenTypeRefresh,
	})
	if err != nil {
		return signInResponse{}, err
	}

	hash := sha256.Sum256([]byte(refreshToken))
	err = store.SessionStore.CreateToken(r.Context(), driftdb.RefreshToken{
		UserID:      user.ID,
		HashedToken: hash[:],
		ExpiresAt:   refreshPayload.ExpiresAt.Time,
		ClientIP:    r.RemoteAddr,
		UserAgent:   r.UserAgent(),
		CreatedAt:   refreshPayload.IssuedAt.Time,
		IsBlocked:   false,
		TokenType:   refreshPayload.TokenType,
	})
	if err != nil {
		return signInResponse{}, err
	}

	return signInResponse{
		User:                  userResponse{ID: user.ID, Email: user.Email, CreatedAt: user.CreatedAt},
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessPayload.ExpiresAt.Time,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshPayload.ExpiresAt.Time,
	}, nil
}

func (a *App) writeJSON(w http.ResponseWriter, status int, body any) {
	if err := util.WriteJSON(w, status, body); err != nil {
		a.log.Error().Err(err).Msg("failed to write JSON response")
	}
}

func (a *App) RefreshToken(w http.ResponseWriter, r *http.Request) error {
	var req refreshTokenRequest
	if err := util.ReadJSON(r, &req); err != nil {
		return util.NewError(http.StatusBadRequest, errors.New("invalid request payload"))
	}

	if err := util.NewValidate(req); err != nil {
		return util.NewError(http.StatusBadRequest, err)
	}

	payload, err := a.jwtMaker.VerifyToken(req.RefreshToken)
	if err != nil || payload.TokenType != util.TokenTypeRefresh {
		return util.NewError(http.StatusUnauthorized, errors.New("invalid refresh token"))
	}

	hash := sha256.Sum256([]byte(req.RefreshToken))
	storedToken, err := a.Store.SessionStore.Get(r.Context(), payload.UserID, hash[:], payload.TokenType)
	if err != nil {
		if errors.Is(err, driftdb.ErrRefreshTokenNotFound) || errors.Is(err, driftdb.ErrRefreshTokenBlocked) {
			return util.NewError(http.StatusUnauthorized, errors.New("invalid refresh token"))
		}
		return err
	}

	if time.Now().After(storedToken.ExpiresAt) {
		return util.NewError(http.StatusUnauthorized, errors.New("invalid refresh token"))
	}

	user, err := a.Store.UserStore.GetUserByID(r.Context(), payload.UserID)
	if err != nil {
		return err
	}

	var response signInResponse
	err = a.Store.WithTX(r.Context(), func(store *driftdb.Store) error {
		if err := store.SessionStore.Delete(r.Context(), payload.UserID, hash[:]); err != nil {
			return err
		}

		response, err = a.createSession(r, user, store)
		return err
	})
	if err != nil {
		return err
	}

	a.log.Info().Str("user_id", user.ID.String()).Msg("refresh token used")
	a.writeJSON(w, http.StatusOK, response)
	return nil
}
