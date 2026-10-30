package driftdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"
)

// OutboxEntry is the publication intent for one report job.
//
// ClaimToken is only populated by ClaimPendingReports. A zero ClaimToken means
// the entry is not currently claimed by this publisher. Attempts and
// SendFailures are populated by both claim and read queries.
type OutboxEntry struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	ReportID     uuid.UUID
	ReportType   string
	ClaimToken   uuid.UUID
	Attempts     int
	SendFailures int
}

type OutboxDB struct {
	db DBTX
}

func NewOutboxDB(db *sql.DB) *OutboxDB {
	return &OutboxDB{db: db}
}

// Enqueue records the intent to publish a report job. It is meant to run in the
// same transaction as the report insert, so the report and its publication are
// committed together.
func (d *OutboxDB) Enqueue(ctx context.Context, entry OutboxEntry) error {
	stmt := `
		INSERT INTO report_outbox (id, user_id, report_id, report_type)
		VALUES ($1, $2, $3, $4)
	`

	_, err := d.db.ExecContext(ctx, stmt, entry.ID, entry.UserID, entry.ReportID, entry.ReportType)
	if err != nil {
		return fmt.Errorf("enqueue outbox: %w", err)
	}

	return nil
}

// GetPendingReports returns entries that are currently eligible for a claim.
// It is a read-only helper for tests and operational inspection; the publisher
// uses ClaimPendingReports instead.
func (d *OutboxDB) GetPendingReports(ctx context.Context, limit int) ([]OutboxEntry, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("pending outbox: limit must be positive")
	}

	stmt := `
		SELECT id, user_id, report_id, report_type, attempts, send_failures
		FROM report_outbox
		WHERE published_at IS NULL
		AND dead_at IS NULL
		AND next_attempt_at <= CURRENT_TIMESTAMP
		AND (lease_expires_at IS NULL OR lease_expires_at <= CURRENT_TIMESTAMP)
		ORDER BY created_at
		LIMIT $1;
	`

	rows, err := d.db.QueryContext(ctx, stmt, limit)
	if err != nil {
		return nil, fmt.Errorf("pending outbox: %w", err)
	}
	defer rows.Close()

	entries, err := scanOutboxEntries(rows)
	if err != nil {
		return nil, fmt.Errorf("pending outbox: %w", err)
	}

	return entries, nil
}

// ClaimPendingReports atomically claims up to limit eligible entries.
//
// The row locks are held only for this statement. After it completes, the
// persisted claim token and lease prevent other publishers from sending the
// same rows until the lease expires. The caller must send to SQS after this
// method returns; it must not keep a database transaction open across the
// network call.
func (d *OutboxDB) ClaimPendingReports(ctx context.Context, limit int, lease time.Duration) ([]OutboxEntry, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("claim pending outbox: limit must be positive")
	}
	if lease <= 0 {
		return nil, fmt.Errorf("claim pending outbox: lease must be positive")
	}

	claimToken := uuid.New()

	stmt := `
		WITH picked AS (
			SELECT id
			FROM report_outbox
			WHERE published_at IS NULL
			AND dead_at IS NULL
			AND next_attempt_at <= CURRENT_TIMESTAMP
			AND (lease_expires_at IS NULL OR lease_expires_at <= CURRENT_TIMESTAMP)
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE report_outbox AS o
		SET claim_token = $2,
			claimed_at = CURRENT_TIMESTAMP,
			lease_expires_at = CURRENT_TIMESTAMP + ($3 * INTERVAL '1 second'),
			attempts = o.attempts + 1
		FROM picked
		WHERE o.id = picked.id
		AND o.published_at IS NULL
		AND o.dead_at IS NULL
		RETURNING o.id, o.user_id, o.report_id, o.report_type, o.attempts, o.send_failures;
	`

	rows, err := d.db.QueryContext(ctx, stmt, limit, claimToken, lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim pending outbox: %w", err)
	}
	defer rows.Close()

	entries, err := scanOutboxEntries(rows)
	if err != nil {
		return nil, fmt.Errorf("claim pending outbox: %w", err)
	}

	for i := range entries {
		entries[i].ClaimToken = claimToken
	}

	return entries, nil
}

func scanOutboxEntries(rows *sql.Rows) ([]OutboxEntry, error) {
	var entries []OutboxEntry

	for rows.Next() {
		var entry OutboxEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.ReportID,
			&entry.ReportType,
			&entry.Attempts,
			&entry.SendFailures,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// MarkPublished stamps an entry as sent. The claim token makes this a
// compare-and-set: a publisher whose lease expired cannot mark a row that was
// reclaimed by another publisher.
func (d *OutboxDB) MarkPublished(ctx context.Context, id uuid.UUID, claimToken uuid.UUID) error {
	if claimToken == uuid.Nil() {
		return fmt.Errorf("markpublished: claim token is required")
	}

	stmt := `
		UPDATE report_outbox
		SET published_at = CURRENT_TIMESTAMP,
			claim_token = NULL,
			claimed_at = NULL,
			lease_expires_at = NULL,
			last_error = NULL
		WHERE id = $1
		AND claim_token = $2
		AND published_at IS NULL
		AND dead_at IS NULL;
	`

	result, err := d.db.ExecContext(ctx, stmt, id, claimToken)
	if err != nil {
		return fmt.Errorf("markpublished: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("markpublished: get affected rows: %w", err)
	}
	if rowsAffected == 1 {
		return nil
	}

	// A retry of MarkPublished after a lost response is safe: the row may
	// already have been published by this claim or by a newer one. It is only a
	// lost claim when the row still exists and is not published.
	var published bool
	err = d.db.QueryRowContext(
		ctx,
		`SELECT published_at IS NOT NULL FROM report_outbox WHERE id = $1`,
		id,
	).Scan(&published)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("markpublished: %w", ErrOutboxClaimLost)
	}
	if err != nil {
		return fmt.Errorf("markpublished: check published state: %w", err)
	}
	if published {
		return nil
	}

	return fmt.Errorf("markpublished: %w", ErrOutboxClaimLost)
}

// ReleaseClaim makes a failed entry eligible for another attempt after
// nextAttempt. countFailure controls whether this failure counts toward the
// per-entry permanent-failure budget. Transient and global infrastructure
// failures deliberately do not consume that budget.
func (d *OutboxDB) ReleaseClaim(
	ctx context.Context,
	id uuid.UUID,
	claimToken uuid.UUID,
	nextAttempt time.Time,
	errorMessage string,
	countFailure bool,
) error {
	if claimToken == uuid.Nil() {
		return fmt.Errorf("release outbox claim: claim token is required")
	}

	stmt := `
		UPDATE report_outbox
		SET claim_token = NULL,
			claimed_at = NULL,
			lease_expires_at = NULL,
			next_attempt_at = $3,
			send_failures = send_failures + CASE WHEN $5 THEN 1 ELSE 0 END,
			last_error = $4
		WHERE id = $1
		AND claim_token = $2
		AND published_at IS NULL
		AND dead_at IS NULL;
	`

	result, err := d.db.ExecContext(
		ctx,
		stmt,
		id,
		claimToken,
		nextAttempt,
		errorMessage,
		countFailure,
	)
	if err != nil {
		return fmt.Errorf("release outbox claim: %w", err)
	}

	return requireOutboxClaim(result, "release outbox claim")
}

// MarkDead stops automatic retries for an entry that cannot be published with
// the current payload. The row remains in report_outbox for inspection and can
// be replayed with ReplayDead.
func (d *OutboxDB) MarkDead(
	ctx context.Context,
	id uuid.UUID,
	claimToken uuid.UUID,
	errorMessage string,
) error {
	if claimToken == uuid.Nil() {
		return fmt.Errorf("mark outbox dead: claim token is required")
	}

	stmt := `
		UPDATE report_outbox
		SET claim_token = NULL,
			claimed_at = NULL,
			lease_expires_at = NULL,
			dead_at = CURRENT_TIMESTAMP,
			send_failures = send_failures + 1,
			last_error = $3
		WHERE id = $1
		AND claim_token = $2
		AND published_at IS NULL
		AND dead_at IS NULL;
	`

	result, err := d.db.ExecContext(ctx, stmt, id, claimToken, errorMessage)
	if err != nil {
		return fmt.Errorf("mark outbox dead: %w", err)
	}

	return requireOutboxClaim(result, "mark outbox dead")
}

// ReplayDead makes a dead entry claimable again. It is intentionally a
// separate operation so dead entries are not retried automatically.
func (d *OutboxDB) ReplayDead(ctx context.Context, id uuid.UUID) error {
	stmt := `
		UPDATE report_outbox
		SET dead_at = NULL,
			claim_token = NULL,
			claimed_at = NULL,
			lease_expires_at = NULL,
			next_attempt_at = CURRENT_TIMESTAMP,
			attempts = 0,
			send_failures = 0,
			last_error = NULL
		WHERE id = $1
		AND published_at IS NULL
		AND dead_at IS NOT NULL;
	`

	result, err := d.db.ExecContext(ctx, stmt, id)
	if err != nil {
		return fmt.Errorf("replay dead outbox: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("replay dead outbox: get affected rows: %w", err)
	}
	if rowsAffected != 1 {
		return ErrOutboxNotDead
	}

	return nil
}

func requireOutboxClaim(result sql.Result, operation string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: get affected rows: %w", operation, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("%s: %w", operation, ErrOutboxClaimLost)
	}

	return nil
}
