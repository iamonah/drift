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

type ReportDB struct {
	db DBTX
}

func NewReportDB(db *sql.DB) *ReportDB {
	return &ReportDB{db: db}
}

type Report struct {
	UserID               uuid.UUID
	ID                   uuid.UUID
	ReportType           string
	OutputFilePath       sql.NullString
	DownloadURL          sql.NullString
	DownloadURLExpiresAt sql.NullTime
	ErrorMessage         sql.NullString
	CreatedAt            time.Time
	StartedAt            sql.NullTime
	FailedAt             sql.NullTime
	CompletedAt          sql.NullTime
}

func (d *ReportDB) CreateReport(ctx context.Context, report Report) error {
	stmt := `
		INSERT INTO reports (user_id,id,report_type) VALUES ($1, $2, $3)
	`
	_, err := d.db.ExecContext(ctx, stmt, report.UserID, report.ID, report.ReportType)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && string(pqErr.Code) == "23505" {
			return fmt.Errorf("createreport: this report %s already exists", report.ID)
		}
		return fmt.Errorf("createreport: %w", err)
	}
	return nil
}

func (d *ReportDB) GetReport(ctx context.Context, userID uuid.UUID, id uuid.UUID) (Report, error) {
	stmt := `
		SELECT
			user_id,
			id,
			report_type,
			output_file_path,
			download_url,
			download_url_expires_at,
			error_message,
			created_at,
			started_at,
			failed_at,
			completed_at
		FROM reports WHERE user_id = $1 AND id = $2
	`

	var report Report

	err := d.db.QueryRowContext(ctx, stmt, userID, id).Scan(
		&report.UserID,
		&report.ID,
		&report.ReportType,
		&report.OutputFilePath,
		&report.DownloadURL,
		&report.DownloadURLExpiresAt,
		&report.ErrorMessage,
		&report.CreatedAt,
		&report.StartedAt,
		&report.FailedAt,
		&report.CompletedAt,
	)

	return report, err
}
func (d *ReportDB) GetReportsByUser(ctx context.Context, userID uuid.UUID) ([]Report, error) {
	stmt := `
		SELECT
			user_id,
			id,
			report_type,
			output_file_path,
			download_url,
			download_url_expires_at,
			error_message,
			created_at,
			started_at,
			failed_at,
			completed_at
		FROM reports
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := d.db.QueryContext(ctx, stmt, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []Report

	for rows.Next() {
		var report Report

		err := rows.Scan(
			&report.UserID,
			&report.ID,
			&report.ReportType,
			&report.OutputFilePath,
			&report.DownloadURL,
			&report.DownloadURLExpiresAt,
			&report.ErrorMessage,
			&report.CreatedAt,
			&report.StartedAt,
			&report.FailedAt,
			&report.CompletedAt,
		)
		if err != nil {
			return nil, err
		}

		reports = append(reports, report)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return reports, nil
}
func (d *ReportDB) MarkStarted(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	stmt := `
		UPDATE reports
		SET started_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND id = $2
	`

	_, err := d.db.ExecContext(ctx, stmt, userID, id)
	return err
}

func (d *ReportDB) MarkFailed(ctx context.Context, userID uuid.UUID, id uuid.UUID, errorMessage string) error {
	stmt := `
		UPDATE reports
		SET
			failed_at = CURRENT_TIMESTAMP,
			error_message = $3
		WHERE user_id = $1
		  AND id = $2
	`

	_, err := d.db.ExecContext(ctx, stmt, userID, id, errorMessage)
	return err
}

func (d *ReportDB) MarkCompleted(ctx context.Context, userID uuid.UUID, id uuid.UUID, outputFilePath string, downloadURL string, expiresAt time.Time) error {
	stmt := `
		UPDATE reports
		SET
			completed_at = CURRENT_TIMESTAMP,
			output_file_path = $3,
			download_url = $4,
			download_url_expires_at = $5
		WHERE user_id = $1
		  AND id = $2
	`

	_, err := d.db.ExecContext(ctx, stmt,
		userID,
		id,
		outputFilePath,
		downloadURL,
		expiresAt,
	)

	return err
}

func (d *ReportDB) Delete(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	stmt := `
		DELETE FROM reports
		WHERE user_id = $1
		  AND id = $2
	`

	_, err := d.db.ExecContext(ctx, stmt, userID, id)
	return err
}
