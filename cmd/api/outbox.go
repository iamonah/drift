package main

import (
	"context"
	"time"

	"github.com/iamonah/drift/internal/job"
)

const (
	outboxInterval  = time.Second * 5
	outboxBatchSize = 20
)

// runOutboxPublisher drains the report outbox until ctx is cancelled. It runs
// in the API process so publishing no longer depends on the request that
// created the report.
func (a *App) runOutboxPublisher(ctx context.Context) {
	a.publishOutbox(ctx) // quick first flush on startup

	ticker := time.NewTicker(outboxInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.publishOutbox(ctx)
		}
	}
}

// publishOutbox sends at most one batch. An entry that is sent but fails to be
// marked stays unpublished and is sent again, which is safe because claiming a
// report is what decides whether the job runs.
func (a *App) publishOutbox(ctx context.Context) {
	entries, err := a.Store.OutboxStore.GetPendingReports(ctx, outboxBatchSize)
	if err != nil {
		a.log.Error().Err(err).Msg("outbox: list pending entries")
		return
	}

	for _, entry := range entries {
		_, err := a.awsSQS.SendMessage(ctx, a.queueURL, job.Report{
			ID:     entry.ReportID,
			UserID: entry.UserID,
			Type:   entry.ReportType,
		}, nil)
		if err != nil {
			a.log.Error().Err(err).Str("report_id", entry.ReportID.String()).Msg("outbox: send message")
			continue
		}

		if err := a.Store.OutboxStore.MarkPublished(ctx, entry.ID); err != nil {
			a.log.Error().Err(err).Str("report_id", entry.ReportID.String()).Msg("outbox: mark published")
		}
	}
}
