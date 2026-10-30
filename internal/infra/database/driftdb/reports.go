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

	// ClaimToken is the token assigned by ClaimReport for the current attempt.
	// It is zero on reports read through other queries.
	ClaimToken uuid.UUID
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

// reportAttemptsExhaustedMessage is stored on a report that can no longer be
// claimed because it has used its processing attempt budget.
const reportAttemptsExhaustedMessage = "report processing attempts exhausted"

// ClaimReport atomically claims a pending report, or a processing report whose
// lease has expired, and returns its updated data.
//
// Every successful claim assigns a fresh UUID claim token, returned in
// Report.ClaimToken, which the worker must present for lease renewal and for
// terminal transitions. A processing report with no lease expiration is treated
// as expired so that reports left in an inconsistent state can be recovered.
//
// A report whose attempt budget is exhausted cannot be claimed again. When it is
// pending, or processing with no live lease, it is atomically marked failed and
// nil, nil is returned. A report still inside another worker's lease returns
// ErrReportProcessing, and a report that does not exist returns
// ErrReportNotFound. A terminal report (completed or failed) returns nil, nil.
func (d *ReportDB) ClaimReport(ctx context.Context, userID uuid.UUID, reportID uuid.UUID, lease time.Duration) (*Report, error) {
	if lease <= 0 {
		return nil, errors.New("claim report: lease must be positive")
	}

	claimToken := uuid.New()

	stmt := `
		UPDATE reports
		SET
			status = 'processing',
			claim_token = $3,
			started_at = CURRENT_TIMESTAMP,
			lease_expires_at = CURRENT_TIMESTAMP + ($4 * INTERVAL '1 second'),
			processing_attempts = processing_attempts + 1,
			failed_at = NULL,
			error_message = NULL
		WHERE user_id = $1
		AND id = $2
		AND processing_attempts < processing_limit
		AND (
			status = 'pending'
			OR (
				status = 'processing'
				AND (
					lease_expires_at IS NULL
					OR lease_expires_at <= CURRENT_TIMESTAMP
				)
			)
		)
		RETURNING ` + reportColumns

	report, err := scanReport(d.db.QueryRowContext(
		ctx,
		stmt,
		userID,
		reportID,
		claimToken,
		lease.Seconds(),
	))
	if err == nil {
		report.ClaimToken = claimToken
		return &report, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("claim report: %w", err)
	}

	// The claim matched nothing. If the attempt budget is exhausted, no worker
	// can ever claim the report again, so terminalize it before returning no
	// work. The status and lease guards make this safe under concurrent workers:
	// only one row update can win, a live lease is left to its owner, and a
	// stale worker's token-fenced writes fail once the report is failed.
	result, err := d.db.ExecContext(
		ctx,
		`UPDATE reports
		SET
			status = 'failed',
			failed_at = CURRENT_TIMESTAMP,
			error_message = $3,
			claim_token = NULL,
			lease_expires_at = NULL
		WHERE user_id = $1
		AND id = $2
		AND processing_attempts >= processing_limit
		AND (
			status = 'pending'
			OR (
				status = 'processing'
				AND (
					lease_expires_at IS NULL
					OR lease_expires_at <= CURRENT_TIMESTAMP
				)
			)
		)`,
		userID,
		reportID,
		reportAttemptsExhaustedMessage,
	)
	if err != nil {
		return nil, fmt.Errorf("claim report: fail exhausted report: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("claim report: fail exhausted report: get affected rows: %w", err)
	}
	if rowsAffected == 1 {
		return nil, nil
	}

	// Nothing was claimed or terminalized, so check whether another worker
	// still owns a valid lease, whether the report is terminal, or whether it is
	// gone.
	var status ReportStatus
	err = d.db.QueryRowContext(
		ctx,
		`SELECT status FROM reports WHERE user_id = $1 AND id = $2`,
		userID,
		reportID,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReportNotFound
		}
		return nil, fmt.Errorf("claim report: read status: %w", err)
	}

	switch status {
	case ReportStatusCompleted, ReportStatusFailed:
		// A terminal report is not an error. Returning nil indicates that
		// there is no work left to do, allowing the caller to acknowledge
		// the SQS message without processing the report again.
		return nil, nil
	case ReportStatusProcessing:
		return nil, ErrReportProcessing
	default:
		// A pending report that survived both updates is not in a state this
		// method can safely acknowledge. Return an error so its message is
		// redelivered rather than discarded while the report is non-terminal.
		return nil, fmt.Errorf("claim report: report %s is pending but not claimable", reportID)
	}
}

// UpdateLease extends the lease of a report only while claimToken still owns it.
// A report claimed by another worker returns ErrReportOwnershipLost.
func (d *ReportDB) UpdateLease(ctx context.Context, userID uuid.UUID, id uuid.UUID, claimToken uuid.UUID, lease time.Duration) error {
	if lease <= 0 {
		return errors.New("update lease: lease must be positive")
	}

	stmt := `
		UPDATE reports
		SET lease_expires_at = CURRENT_TIMESTAMP + ($4 * INTERVAL '1 second')
		WHERE user_id = $1 AND id = $2 AND status = 'processing' AND claim_token = $3
	`

	result, err := d.db.ExecContext(ctx, stmt, userID, id, claimToken, lease.Seconds())
	if err != nil {
		return fmt.Errorf("update lease: %w", err)
	}

	return requireOwnedRow(result, "update lease")
}

// MarkFailed records a failure only while claimToken still owns the report.
// A stale worker cannot overwrite a newer attempt's state.
func (d *ReportDB) MarkFailed(ctx context.Context, userID uuid.UUID, id uuid.UUID, claimToken uuid.UUID, errorMessage string) error {
	stmt := `
		UPDATE reports SET
			status = $3,
			failed_at = CURRENT_TIMESTAMP,
			error_message = $4
		WHERE user_id = $1 AND id = $2 AND status = 'processing' AND claim_token = $5
	`

	result, err := d.db.ExecContext(
		ctx, stmt,
		userID, id,
		ReportStatusFailed,
		errorMessage,
		claimToken,
	)
	if err != nil {
		return fmt.Errorf("markfailed: %w", err)
	}

	return requireOwnedRow(result, "markfailed")
}

// MarkCompleted completes a report only while claimToken still owns it.
// A stale worker cannot overwrite a newer attempt's state.
func (d *ReportDB) MarkCompleted(ctx context.Context, userID uuid.UUID, id uuid.UUID, claimToken uuid.UUID, outputObjectKey string) error {
	stmt := `
		UPDATE reports SET
			status = $3,
			completed_at = CURRENT_TIMESTAMP,
			output_object_key = $4,
			error_message = NULL
		WHERE user_id = $1 AND id = $2 AND status = 'processing' AND claim_token = $5
	`

	result, err := d.db.ExecContext(
		ctx, stmt,
		userID, id,
		ReportStatusCompleted,
		outputObjectKey,
		claimToken,
	)
	if err != nil {
		return fmt.Errorf("markcompleted: %w", err)
	}

	return requireOwnedRow(result, "markcompleted")
}

// requireOwnedRow treats a conditional update that matched no row as a lost
// claim, so callers can tell it apart from a database failure.
func requireOwnedRow(result sql.Result, operation string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: get affected rows: %w", operation, err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("%s: %w", operation, ErrReportOwnershipLost)
	}

	return nil
}

func (d *ReportDB) Delete(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	stmt := `DELETE FROM reports WHERE user_id = $1 AND id = $2`

	_, err := d.db.ExecContext(ctx, stmt, userID, id)
	if err != nil {
		return fmt.Errorf("delete report: %w", err)
	}

	return nil
}
