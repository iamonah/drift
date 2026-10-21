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
)

// reportClaimLease is how long a report may stay processing before another
// worker is allowed to reclaim it. It must be longer than the slowest expected
// report, because the lease is not renewed while a worker is busy.
const reportClaimLease = 5 * time.Minute

func (w *Worker) ProcessReport(ctx context.Context, userID uuid.UUID, reportID uuid.UUID) (err error) {
	report, err := w.reportStore.ReportStore.ClaimReport(ctx, userID, reportID, reportClaimLease)
	if err != nil {
		return fmt.Errorf("claimreport: %w", err)
	}

	if report == nil {
		//probably another worker claimed the report, or the report is not pending anymore.
		w.log.Info().Str("report_id", reportID.String()).Msg("report was not claimed; skipping")
		return nil
	}

	defer func() {
		if err == nil {
			return
		}

		w.log.Error().Err(err).Str("report_id", reportID.String()).Msg("report processing failed")

		// Record the failure even if the job context was cancelled (shutdown or
		// timeout), otherwise the report would be left in processing.
		if markErr := w.reportStore.ReportStore.MarkFailed(context.WithoutCancel(ctx), userID, reportID, err.Error()); markErr != nil {
			err = errors.Join(err, fmt.Errorf("markfailed: %w", markErr))
		}
	}()

	// Processing report begins.
	if report.ReportType != driftdb.ReportTypeMonsters.String() {
		return fmt.Errorf("unsupported report type: %s", report.ReportType)
	}

	monsters, err := w.loz.GetMonsters(ctx)
	if err != nil {
		return fmt.Errorf("get monsters from loz: %w", err)
	}

	if len(monsters.Data) == 0 {
		return errors.New("no monsters data found")
	}

	csvData, err := generateCSV(monsters.Data)
	if err != nil {
		return fmt.Errorf("generate CSV: %w", err)
	}

	objectKey := fmt.Sprintf("users/%s/reports/%s.csv.gz", userID, reportID)
	if err := w.ObjectStorage.UploadObject(ctx, w.cfg.AWSS3.BucketName, objectKey, csvData, "application/gzip"); err != nil {
		return fmt.Errorf("upload CSV to S3: %w", err)
	}

	if err := w.reportStore.ReportStore.MarkCompleted(ctx, userID, reportID, objectKey); err != nil {
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
