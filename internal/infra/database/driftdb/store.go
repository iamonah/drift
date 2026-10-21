package driftdb

import (
	"context"
	"database/sql"
	"uuid"
)

type reportStore interface {
	CreateReport(ctx context.Context, report Report) (Report, error)
	GetReportByUserID(ctx context.Context, userID uuid.UUID, id uuid.UUID) (Report, error)
	// GetReportsByUser(ctx context.Context, userID uuid.UUID) ([]Report, error)
	MarkStarted(ctx context.Context, userID uuid.UUID, id uuid.UUID) error
	MarkFailed(ctx context.Context, userID uuid.UUID, id uuid.UUID, errorMessage string) error
	MarkCompleted(ctx context.Context, userID uuid.UUID, id uuid.UUID, outputObjectKey string) error
	Delete(ctx context.Context, userID uuid.UUID, id uuid.UUID) error
}

type userStore interface {
	InsertUser(ctx context.Context, user User) (*User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	UpdateEmail(ctx context.Context, id uuid.UUID, email string) error
	UpdatePassword(ctx context.Context, id uuid.UUID, hashedPassword []byte) error
	DeleteUser(ctx context.Context, id uuid.UUID) error
}

type refreshStore interface {
	CreateToken(ctx context.Context, token RefreshToken) error
	Get(ctx context.Context, userID uuid.UUID, hashedToken []byte, tokenType string) (RefreshToken, error)
	Delete(ctx context.Context, userID uuid.UUID, hashedToken []byte) error
	DeleteAll(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context) error
}

type outboxStore interface {
	Enqueue(ctx context.Context, entry OutboxEntry) error
	Pending(ctx context.Context, limit int) ([]OutboxEntry, error)
	MarkPublished(ctx context.Context, id uuid.UUID) error
}

type Store struct {
	db           *sql.DB
	ReportStore  reportStore
	UserStore    userStore
	SessionStore refreshStore
	OutboxStore  outboxStore
}

func NewStore(db *sql.DB) *Store {
	return newStore(db, db)
}

func newStore(db *sql.DB, dbtx DBTX) *Store {
	return &Store{
		db:           db,
		ReportStore:  &ReportDB{db: dbtx},
		UserStore:    &UserDB{db: dbtx},
		SessionStore: &RefreshTokenDB{db: dbtx},
		OutboxStore:  &OutboxDB{db: dbtx},
	}
}

func (s *Store) WithTX(ctx context.Context, fn func(store *Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	store := newStore(s.db, tx)

	if err := fn(store); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return rbErr
		}
		return err
	}

	return tx.Commit()
}

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
