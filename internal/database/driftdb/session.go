package driftdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"
)

type RefreshTokenDB struct {
	db DBTX
}

func NewRefreshTokenDB(db *sql.DB) *RefreshTokenDB {
	return &RefreshTokenDB{db: db}
}

type RefreshToken struct {
	UserID      uuid.UUID
	HashedToken []byte
	ExpiresAt   time.Time
	ClientIP    string
	CreatedAt   time.Time
	TokenType   string
	UserAgent   string
	IsBlocked   bool
}

func (d *RefreshTokenDB) CreateToken(ctx context.Context, token RefreshToken) error {
	stmt := `
		INSERT INTO refresh_tokens (
			user_id,
			hashed_token,
			expires_at,
			client_ip,
			created_at,
			token_type,
			user_agent,
			is_blocked
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := d.db.ExecContext(ctx, stmt,
		token.UserID,
		token.HashedToken,
		token.ExpiresAt,
		token.ClientIP,
		token.CreatedAt,
		token.TokenType,
		token.UserAgent,
		token.IsBlocked,
	)
	if err != nil {
		return fmt.Errorf("createtoken: %w", err)
	}

	return nil
}

func (d *RefreshTokenDB) Get(ctx context.Context, userID uuid.UUID, hashedToken []byte, tokenType string) (RefreshToken, error) {
	stmt := `
		SELECT user_id, hashed_token, expires_at, client_ip, created_at, token_type, user_agent, is_blocked
		FROM refresh_tokens
		WHERE user_id = $1
		  AND hashed_token = $2
		  AND token_type = $3
	`

	var token RefreshToken

	err := d.db.QueryRowContext(ctx, stmt, userID, hashedToken, tokenType).Scan(
		&token.UserID,
		&token.HashedToken,
		&token.ExpiresAt,
		&token.ClientIP,
		&token.CreatedAt,
		&token.TokenType,
		&token.UserAgent,
		&token.IsBlocked,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return token, ErrRefreshTokenNotFound
		}

		return token, fmt.Errorf("gettoken: %w", err)
	}

	if token.IsBlocked {
		return token, ErrRefreshTokenBlocked
	}

	return token, nil
}

func (d *RefreshTokenDB) Delete(ctx context.Context, userID uuid.UUID, hashedToken []byte) error {
	stmt := `
		DELETE FROM refresh_tokens
		WHERE user_id = $1
		  AND hashed_token = $2
	`

	_, err := d.db.ExecContext(ctx, stmt, userID, hashedToken)
	return err
}

func (d *RefreshTokenDB) DeleteAll(ctx context.Context, userID uuid.UUID) error {
	stmt := `
		DELETE FROM refresh_tokens
		WHERE user_id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, userID)
	if err != nil {
		return fmt.Errorf("deleteall: %w", err)
	}

	return nil
}

func (d *RefreshTokenDB) DeleteExpired(ctx context.Context) error {
	stmt := `
		DELETE FROM refresh_tokens
		WHERE expires_at <= CURRENT_TIMESTAMP
	`

	_, err := d.db.ExecContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("deleteexpired: %w", err)
	}

	return nil
}
