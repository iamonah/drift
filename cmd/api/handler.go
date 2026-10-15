package main

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/gorilla/mux"
	cfg "github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
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
	User  userResponse `json:"user"`
	Token token        `json:"token"`
}

type token struct {
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

func (a *App) SignUp() http.HandlerFunc {
	return a.handler(func(w http.ResponseWriter, r *http.Request) error {
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
	})
}

func (a *App) SignIn() http.HandlerFunc {
	return a.handler(func(w http.ResponseWriter, r *http.Request) error {
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

		response, err := a.createSession(r, user.ID, a.Store)
		if err != nil {
			return err
		}

		a.log.Info().Str("user_id", user.ID.String()).Msg("user signed in")
		a.writeJSON(w, http.StatusOK, signInResponse{
			User: userResponse{
				ID:        user.ID,
				Email:     user.Email,
				CreatedAt: user.CreatedAt,
			},
			Token: response,
		})
		return nil
	})
}

func (a *App) createSession(r *http.Request, userID uuid.UUID, store *driftdb.Store) (token, error) {
	accessDuration, err := cfg.ParseDuration(a.cfg.JWT.AccessTokenDuration)
	if err != nil {
		return token{}, err
	}
	refreshDuration, err := cfg.ParseDuration(a.cfg.JWT.RefreshTokenDuration)
	if err != nil {
		return token{}, err
	}

	accessToken, accessPayload, err := a.jwtMaker.GenerateToken(util.JWTData{
		UserID:      userID,
		Duration:    accessDuration,
		ServiceName: a.cfg.JWT.Issuer,
		Audience:    a.cfg.JWT.Audience,
		TokenType:   util.TokenTypeAccess,
	})
	if err != nil {
		return token{}, err
	}
	refreshToken, refreshPayload, err := a.jwtMaker.GenerateToken(util.JWTData{
		UserID:      userID,
		Duration:    refreshDuration,
		ServiceName: a.cfg.JWT.Issuer,
		Audience:    a.cfg.JWT.Audience,
		TokenType:   util.TokenTypeRefresh,
	})
	if err != nil {
		return token{}, err
	}

	hash := sha256.Sum256([]byte(refreshToken))
	err = store.SessionStore.CreateToken(r.Context(), driftdb.RefreshToken{
		UserID:      userID,
		HashedToken: hash[:],
		ExpiresAt:   refreshPayload.ExpiresAt.Time,
		ClientIP:    r.RemoteAddr,
		UserAgent:   r.UserAgent(),
		CreatedAt:   refreshPayload.IssuedAt.Time,
		IsBlocked:   false,
		TokenType:   refreshPayload.TokenType,
	})
	if err != nil {
		return token{}, err
	}

	return token{
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

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (a *App) RefreshToken() http.HandlerFunc {
	return a.handler(func(w http.ResponseWriter, r *http.Request) error {
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

		var tokenResp token
		err = a.Store.WithTX(r.Context(), func(store *driftdb.Store) error {
			if err := store.SessionStore.Delete(r.Context(), payload.UserID, hash[:]); err != nil {
				return err
			}

			tokenResp, err = a.createSession(r, payload.UserID, store)
			return err
		})
		if err != nil {
			return err
		}

		a.log.Info().Str("user_id", payload.UserID.String()).Msg("refresh token used")
		a.writeJSON(w, http.StatusOK, tokenResp)
		return nil
	})
}

func (a *App) GetReportByID() http.HandlerFunc {
	return a.handler(func(w http.ResponseWriter, r *http.Request) error {
		vars := mux.Vars(r)
		reportID := vars["report_id"]
		uuidReportID, err := uuid.Parse(reportID)
		if err != nil {
			return util.NewError(http.StatusBadRequest, errors.New("invalid report ID"))
		}

		payload, err := GetJWTPayloadCTX(r)
		if err != nil {
			return util.NewError(http.StatusUnauthorized, errors.New("unauthorized"))
		}

		report, err := a.Store.ReportStore.GetReport(r.Context(), payload.UserID, uuidReportID)
		if err != nil {
			if errors.Is(err, driftdb.ErrReportNotFound) {
				return util.NewError(http.StatusNotFound, errors.New("report not found"))
			}
			return err
		}

		a.writeJSON(w, http.StatusOK, report)
		return nil
	})
}

func (a *App) CreateReport() http.HandlerFunc {
	return a.handler(func(w http.ResponseWriter, r *http.Request) error {
		// Placeholder for the reports handler logic
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Reports endpoint"))
		return nil
	})
}
