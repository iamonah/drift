package driftdb

import (
	"context"
	"database/sql"
	"fmt"
	"uuid"
)

// OutboxEntry is the publication intent for one report job.
type OutboxEntry struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	ReportID   uuid.UUID
	ReportType string
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

// Pending returns the oldest unpublished entries, so a publisher can send them
// in the order they were requested.
func (d *OutboxDB) Pending(ctx context.Context, limit int) ([]OutboxEntry, error) {
	stmt := `
		SELECT id, user_id, report_id, report_type
		FROM report_outbox
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
	`

	rows, err := d.db.QueryContext(ctx, stmt, limit)
	if err != nil {
		return nil, fmt.Errorf("pending outbox: %w", err)
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var entry OutboxEntry
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.ReportID, &entry.ReportType); err != nil {
			return nil, fmt.Errorf("pending outbox: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending outbox: %w", err)
	}

	return entries, nil
}

// MarkPublished stamps an entry as sent. An entry that was already published is
// left untouched, so the publisher may safely retry.
func (d *OutboxDB) MarkPublished(ctx context.Context, id uuid.UUID) error {
	stmt := `
		UPDATE report_outbox SET published_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND published_at IS NULL
	`

	if _, err := d.db.ExecContext(ctx, stmt, id); err != nil {
		return fmt.Errorf("markpublished: %w", err)
	}

	return nil
}
