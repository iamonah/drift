package driftdb

import (
	"context"
	"database/sql"
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
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

func (d *RefreshTokenDB) CreateToken(ctx context.Context, token RefreshToken) error {
	stmt := `
		INSERT INTO refresh_tokens (
			user_id,
			hashed_token,
			expires_at
		)
		VALUES ($1, $2, $3)
	`

	_, err := d.db.ExecContext(ctx, stmt,
		token.UserID,
		token.HashedToken,
		token.ExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("createtoken: %w", err)
	}
	return nil
}

func (d *RefreshTokenDB) Get(ctx context.Context, userID uuid.UUID, hashedToken []byte) (RefreshToken, error) {
	stmt := `
		SELECT user_id, hashed_token, created_at, expires_at
		FROM refresh_tokens
		WHERE user_id = $1
		  AND hashed_token = $2
	`

	var token RefreshToken

	err := d.db.QueryRowContext(ctx, stmt, userID, hashedToken).Scan(
		&token.UserID,
		&token.HashedToken,
		&token.CreatedAt,
		&token.ExpiresAt,
	)

	if err != nil {
		return token, fmt.Errorf("gettoken: %w", err)
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
