package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/iamonah/drift/internal/client/loz"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/retry"
)

// reportClaimLease is how long a report may stay processing before another
// worker is allowed to reclaim it. A live worker renews it periodically, but it
// must still cover the time before the first renewal and any stalled work.
const reportClaimLease = 3 * time.Minute

// transientError marks a failure that should be recovered by letting the report
// lease expire and the SQS message be redelivered. It must not be terminalized.
type transientError struct {
	err error
}

func (e *transientError) Error() string { return e.err.Error() }

func (e *transientError) Unwrap() error { return e.err }

func transient(err error) error { return &transientError{err: err} }

func isTransientError(err error) bool {
	var transientErr *transientError
	return errors.As(err, &transientErr) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

func (w *Worker) ProcessReport(ctx context.Context, userID uuid.UUID, reportID uuid.UUID) (err error) {
	report, err := w.reportStore.ReportStore.ClaimReport(ctx, userID, reportID, reportClaimLease)
	if err != nil {
		return fmt.Errorf("claimreport: %w", err)
	}

	if report == nil {
		w.log.Info().Str("report_id", reportID.String()).Msg("report was not claimed; no processing required")
		return nil
	}

	claimToken := report.ClaimToken

	// Create a child context so we can stop processing if ownership is lost.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	leaseRenewInterval := w.leaseRenewInterval
	if leaseRenewInterval <= 0 {
		leaseRenewInterval = reportClaimLease - time.Minute
	}
	leaseTicker := time.NewTicker(leaseRenewInterval)
	defer leaseTicker.Stop()

	go func() {
		for {
			select {
			case <-leaseTicker.C:
				err := w.reportStore.ReportStore.UpdateLease(ctx, userID, reportID, claimToken, reportClaimLease)
				if err == nil {
					continue
				}

				if errors.Is(err, driftdb.ErrReportOwnershipLost) {
					w.log.Warn().Str("report_id", reportID.String()).Msg("report ownership lost; stopping processing")
					cancel(driftdb.ErrReportOwnershipLost)
					return
				}

				// A transient database error does not necessarily mean ownership was lost.
				w.log.Error().Err(err).Str("report_id", reportID.String()).Msg("failed to renew report lease")

			case <-ctx.Done():
				return
			}
		}
	}()

	// Stop renewal before handling a processing failure.
	defer func() {
		if err == nil {
			return
		}

		cancel(nil) // stop the lease renewal goroutine

		if errors.Is(context.Cause(ctx), driftdb.ErrReportOwnershipLost) {
			err = driftdb.ErrReportOwnershipLost
			w.log.Warn().Str("report_id", reportID.String()).Msg("report ownership lost; not marking failed")
			return
		}

		if isTransientError(err) {
			w.log.Warn().Err(err).
				Str("report_id", reportID.String()).
				Msg("report left recoverable after transient dependency failure")
			return
		}

		w.log.Error().Err(err).Str("report_id", reportID.String()).Msg("report processing failed")

		// Record the failure even if the job context was cancelled.
		markCtx, markCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer markCancel()

		markErr := w.reportStore.ReportStore.MarkFailed(markCtx, userID, reportID, claimToken, err.Error())
		if markErr == nil {
			return
		}

		if errors.Is(markErr, driftdb.ErrReportOwnershipLost) {
			err = driftdb.ErrReportOwnershipLost
			w.log.Warn().Str("report_id", reportID.String()).Msg("report ownership lost while recording failure")
			return
		}

		err = errors.Join(err, fmt.Errorf("markfailed: %w", markErr))
	}()

	// Processing report begins.
	if report.ReportType != driftdb.ReportTypeMonsters.String() {
		return fmt.Errorf("unsupported report type: %s", report.ReportType)
	}

	var monsters *loz.GetMonstersResponse
	if err := retry.Do(ctx, w.dependencyRetry, w.log, "loz get monsters", func(ctx context.Context) error {
		var callErr error
		monsters, callErr = w.loz.GetMonsters(ctx)
		return callErr
	}); err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}

		lozErr := fmt.Errorf("get monsters from loz: %w", err)
		if retry.IsPermanent(err) {
			return lozErr
		}
		return transient(lozErr)
	}

	// Check whether ownership was lost before generating the CSV.
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}

	if len(monsters.Data) == 0 {
		return errors.New("no monsters data found")
	}

	csvData, err := generateCSV(monsters.Data)
	if err != nil {
		return fmt.Errorf("generate CSV: %w", err)
	}

	// CSV generation may take time, so check whether ownership was lost.
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}

	// Use a distinct object key for each claim attempt. PutObject is atomic, so
	// retrying the same key cannot expose a partially written report object.
	objectKey := fmt.Sprintf("users/%s/reports/%s-%s.csv.gz", userID, reportID, claimToken)
	if err := retry.Do(ctx, w.dependencyRetry, w.log, "upload report to S3", func(ctx context.Context) error {
		return w.ObjectStorage.UploadObject(ctx, w.cfg.AWSS3.BucketName, objectKey, csvData, "application/gzip")
	}); err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}

		uploadErr := fmt.Errorf("upload CSV to S3: %w", err)
		if retry.IsPermanent(err) {
			return uploadErr
		}
		return transient(uploadErr)
	}

	if cause := context.Cause(ctx); cause != nil {
		return cause
	}

	if err := w.reportStore.ReportStore.MarkCompleted(ctx, userID, reportID, claimToken, objectKey); err != nil {
		return fmt.Errorf("markcompleted: %w", err)
	}

	w.log.Info().Str("report_id", reportID.String()).Str("object_key", objectKey).Msg("report processed successfully")
	return nil
}

func generateCSV(monsters []loz.Monster) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	writer := csv.NewWriter(gz)

	if err := writer.Write([]string{
		"ID", "Name", "Category", "Description", "Image",
		"Locations", "Drops", "DLC",
	}); err != nil {
		return nil, fmt.Errorf("write CSV header: %w", err)
	}

	for _, monster := range monsters {
		record := []string{
			strconv.Itoa(monster.ID),
			monster.Name,
			monster.Category,
			monster.Description,
			monster.Image,
			strings.Join(monster.CommonLocations, ", "),
			strings.Join(monster.Drops, ", "),
			strconv.FormatBool(monster.Dlc),
		}

		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("write monster record: %w", err)
		}
	}

	writer.Flush()
	csvErr := writer.Error()

	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}

	if csvErr != nil {
		return nil, fmt.Errorf("flush CSV: %w", csvErr)
	}

	return buf.Bytes(), nil
}
