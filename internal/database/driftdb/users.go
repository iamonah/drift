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
	db *sql.DB
}

func NewUserDB(db *sql.DB) *UserDB {
	return &UserDB{db: db}
}

type User struct {
	ID             uuid.UUID
	Email          string
	HashedPassword []byte
	CreatedAt      time.Time
}

func (d *UserDB) InsertUser(ctx context.Context, user User) error {
	stmt := `
		INSERT INTO users (id, email, hashed_password)
		VALUES ($1, $2, $3)
	`

	_, err := d.db.ExecContext(ctx, stmt,
		user.ID,
		user.Email,
		user.HashedPassword,
	)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && string(pqErr.Code) == "23505" {
			return fmt.Errorf("insertuser: user with email %s already exists", user.Email)
		}
		return fmt.Errorf("insertuser: %w", err)
	}

	return nil
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

	return user, fmt.Errorf("getuserbyid: %w", err)
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

	return user, fmt.Errorf("getuserbyemail: %w", err)
}

func (d *UserDB) UpdateEmail(ctx context.Context, id uuid.UUID, email string) error {
	stmt := `
		UPDATE users
		SET email = $2
		WHERE id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, id, email)
	return fmt.Errorf("updateemail: %w", err)
}

func (d *UserDB) UpdatePassword(ctx context.Context, id uuid.UUID, hashedPassword []byte) error {
	stmt := `
		UPDATE users
		SET hashed_password = $2
		WHERE id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, id, hashedPassword)
	return fmt.Errorf("updatepassword: %w", err)
}

func (d *UserDB) DeleteUser(ctx context.Context, id uuid.UUID) error {
	stmt := `
		DELETE FROM users
		WHERE id = $1
	`

	_, err := d.db.ExecContext(ctx, stmt, id)
	return fmt.Errorf("deleteuser: %w", err)
}
