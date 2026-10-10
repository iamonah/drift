package driftdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/lib/pq"
)

type UserDB struct {
	db DBTX
}

func NewUserDB(db *sql.DB) *UserDB {
	return &UserDB{db: db}
}

type User struct {
	ID             uuid.UUID `json:"id"`
	Email          string    `json:"email"`
	HashedPassword []byte    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

func (d *UserDB) InsertUser(ctx context.Context, user User) (*User, error) {
	stmt := `
		INSERT INTO users (id, email, hashed_password)
		VALUES ($1, $2, $3)
		RETURNING id, email, created_at
	`

	err := d.db.QueryRowContext(ctx, stmt,
		user.ID,
		user.Email,
		user.HashedPassword,
	).Scan(&user.ID, &user.Email, &user.CreatedAt)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && string(pqErr.Code) == "23505" {
			return nil, ErrUserAlreadyExists
		}
		return nil, fmt.Errorf("insertuser: %w", err)
	}

	return &user, nil
}

func (d *UserDB) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	stmt := `
		SELECT id, email, hashed_password, created_at
		FROM users
		WHERE id = $1
	`

	var user User

	err := d.db.QueryRowContext(ctx, stmt, id).Scan(
		&user.ID,
		&user.Email,
		&user.HashedPassword,
		&user.CreatedAt,
	)

	if err != nil {
		return user, fmt.Errorf("getuserbyid: %w", err)
	}
	return user, nil
}

func (d *UserDB) GetUserByEmail(ctx context.Context, email string) (User, error) {
	stmt := `
		SELECT id, email, hashed_password, created_at
		FROM users
		WHERE email = $1
	`

	var user User

	err := d.db.QueryRowContext(ctx, stmt, email).Scan(
		&user.ID,
		&user.Email,
		&user.HashedPassword,
		&user.CreatedAt,
	)

	if err != nil {
		return user, fmt.Errorf("getuserbyemail: %w", err)
	}
	return user, nil
}

func (d *UserDB) UpdateEmail(ctx context.Context, id uuid.UUID, email string) error {
	stmt := `
		UPDATE users
		SET email = $2
		WHERE id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, id, email)
	if err != nil {
		return fmt.Errorf("updateemail: %w", err)
	}
	return nil
}

func (d *UserDB) UpdatePassword(ctx context.Context, id uuid.UUID, hashedPassword []byte) error {
	stmt := `
		UPDATE users
		SET hashed_password = $2
		WHERE id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, id, hashedPassword)
	if err != nil {
		return fmt.Errorf("updatepassword: %w", err)
	}
	return nil
}

func (d *UserDB) DeleteUser(ctx context.Context, id uuid.UUID) error {
	stmt := `
		DELETE FROM users
		WHERE id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, id)
	if err != nil {
		return fmt.Errorf("deleteuser: %w", err)
	}
	return nil
}
