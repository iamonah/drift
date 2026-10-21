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

// ClaimReport atomically claims a pending report, or one that has been left
// processing for longer than lease, and returns its updated data.
//
// The lease is measured from started_at, which holds the start of the current
// attempt rather than the moment the lease expires. A report left processing
// without a started_at cannot be aged, so it is treated as expired.
//
// A report still inside another worker's lease returns ErrReportProcessing. A
// terminal or unknown report returns nil, nil.
func (d *ReportDB) ClaimReport(ctx context.Context, userID uuid.UUID, reportID uuid.UUID, lease time.Duration) (*Report, error) {
	if lease <= 0 {
		return nil, errors.New("claim report: lease must be positive")
	}

	stmt := `
		UPDATE reports SET
			status = $1,
			started_at = CURRENT_TIMESTAMP,
			failed_at = NULL,
			error_message = NULL
		WHERE user_id = $2 AND id = $3
		  AND (
			status = 'pending'
			OR (
				status = 'processing'
				AND (started_at IS NULL OR started_at < CURRENT_TIMESTAMP - ($4::double precision * INTERVAL '1 second'))
			)
		  )
		RETURNING ` + reportColumns

	report, err := scanReport(d.db.QueryRowContext(
		ctx,
		stmt,
		ReportStatusProcessing,
		userID,
		reportID,
		lease.Seconds(),
	))
	if err == nil {
		return &report, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("claim report: %w", err)
	}

	// The claim matched nothing, so find out why: a report that is still being
	// processed must keep its message on the queue for a later delivery.
	var status ReportStatus
	err = d.db.QueryRowContext(
		ctx,
		`SELECT status FROM reports WHERE user_id = $1 AND id = $2`,
		userID,
		reportID,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("claim report: read status: %w", err)
	}

	if status == ReportStatusProcessing {
		return nil, ErrReportProcessing
	}

	return nil, nil
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
