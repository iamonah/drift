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

type ReportType string

var reportTypes = make(map[string]ReportType)

func newReportType(value string) ReportType {
	t := ReportType(value)
	reportTypes[value] = t
	return t
}

var (
	ReportTypeMonsters = newReportType("monsters")
	ReportTypeFoods    = newReportType("foods")
)

func CheckReportType(reportType string) (ReportType, error) {
	t, ok := reportTypes[reportType]
	if !ok {
		return "", ErrInvalidReportType
	}

	return t, nil
}

func (t ReportType) String() string {
	return string(t)
}

type ReportStatus string

var reportStatuses = make(map[string]ReportStatus)

func newReportStatus(value string) ReportStatus {
	s := ReportStatus(value)
	reportStatuses[value] = s
	return s
}

var (
	ReportStatusPending    = newReportStatus("pending")
	ReportStatusProcessing = newReportStatus("processing")
	ReportStatusCompleted  = newReportStatus("completed")
	ReportStatusFailed     = newReportStatus("failed")
)

func CheckReportStatus(status string) (ReportStatus, error) {
	s, ok := reportStatuses[status]
	if !ok {
		return "", ErrInvalidReportStatus
	}

	return s, nil
}

func (s ReportStatus) String() string {
	return string(s)
}

type Report struct {
	UserID          uuid.UUID
	ID              uuid.UUID
	ReportType      string
	Status          ReportStatus
	OutputObjectKey sql.NullString
	ErrorMessage    sql.NullString
	CreatedAt       time.Time
	StartedAt       sql.NullTime
	FailedAt        sql.NullTime
	CompletedAt     sql.NullTime
}

func scanReport(row interface{ Scan(dest ...any) error }) (Report, error) {
	var report Report
	err := row.Scan(
		&report.UserID,
		&report.ID,
		&report.ReportType,
		&report.Status,
		&report.OutputObjectKey,
		&report.ErrorMessage,
		&report.CreatedAt,
		&report.StartedAt,
		&report.FailedAt,
		&report.CompletedAt,
	)
	return report, err
}

const reportColumns = `
	user_id,
	id,
	report_type,
	status,
	output_object_key,
	error_message,
	created_at,
	started_at,
	failed_at,
	completed_at
`

// CreateReport inserts a pending report and returns it as stored, so the caller
// gets the database-generated values (status, created_at).
func (d *ReportDB) CreateReport(ctx context.Context, report Report) (Report, error) {
	reportType, err := CheckReportType(report.ReportType)
	if err != nil {
		return Report{}, err
	}

	stmt := `
		INSERT INTO reports (user_id, id, report_type, status)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + reportColumns

	created, err := scanReport(d.db.QueryRowContext(
		ctx,
		stmt,
		report.UserID,
		report.ID,
		reportType.String(),
		ReportStatusPending,
	))
	if err != nil {
		var pqErr *pq.Error
		// 23505 covers every unique violation, so match the primary key
		// constraint explicitly rather than assuming a duplicate id.
		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "reports_pkey" {
			return Report{}, fmt.Errorf("createreport: report %s already exists: %w", report.ID, err)
		}
		return Report{}, fmt.Errorf("createreport: %w", err)
	}

	return created, nil
}

func (d *ReportDB) GetReportByUserID(ctx context.Context, userID uuid.UUID, id uuid.UUID) (Report, error) {
	stmt := `SELECT ` + reportColumns + `
		FROM reports
		WHERE user_id = $1 AND id = $2
	`

	report, err := scanReport(d.db.QueryRowContext(ctx, stmt, userID, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Report{}, ErrReportNotFound
		}
		return Report{}, fmt.Errorf("getreport: %w", err)
	}

	return report, nil
}

// func (d *ReportDB) GetReportsByUser(ctx context.Context, userID uuid.UUID) ([]Report, error) {
// 	stmt := `
// 		SELECT
// 		user_id,
// 		id,
// 		report_type,
// 		status,
// 		output_object_key,
// 		error_message,
// 		created_at,
// 		started_at,
// 		failed_at,
// 		completed_at
// 		FROM reports
// 		WHERE user_id = $1
// 		ORDER BY created_at DESC
// 	`

// 	rows, err := d.db.QueryContext(ctx, stmt, userID)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer rows.Close()

// 	var userReports []Report

// 	for rows.Next() {
// 		report, err := scanReport(rows)
// 		if err != nil {
// 			return nil, err
// 		}

// 		userReports = append(userReports, report)
// 	}

// 	if err := rows.Err(); err != nil {
// 		return nil, err
// 	}

// 	return userReports, nil
// }

func (d *ReportDB) MarkStarted(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	stmt := `
		UPDATE reports SET
		status = $3,
		started_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND id = $2
	`

	_, err := d.db.ExecContext(ctx, stmt, userID, id, string(ReportStatusProcessing))
	return err
}

func (d *ReportDB) MarkFailed(ctx context.Context, userID uuid.UUID, id uuid.UUID, errorMessage string) error {
	stmt := `
		UPDATE reports SET
		status = $3,
		failed_at = CURRENT_TIMESTAMP,
		error_message = $4
		WHERE user_id = $1 AND id = $2
	`
	_, err := d.db.ExecContext(ctx, stmt, userID, id, string(ReportStatusFailed), errorMessage)
	return err
}

func (d *ReportDB) MarkCompleted(ctx context.Context, userID uuid.UUID, id uuid.UUID, outputObjectKey string) error {
	stmt := `
		UPDATE reports SET
		status = $3,
		completed_at = CURRENT_TIMESTAMP,
		output_object_key = $4
		WHERE user_id = $1 AND id = $2
	`
	_, err := d.db.ExecContext(ctx, stmt, userID, id, string(ReportStatusCompleted), outputObjectKey)
	return err
}

func (d *ReportDB) Delete(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	stmt := `DELETE FROM reports WHERE user_id = $1 AND id = $2`

	_, err := d.db.ExecContext(ctx, stmt, userID, id)
	if err != nil {
		return fmt.Errorf("delete report: %w", err)
	}

	return nil
}
