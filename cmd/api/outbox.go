package main

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/sqs"
	"github.com/iamonah/drift/internal/job"
	"github.com/rs/zerolog"
)

// outboxShutdownGrace bounds how long server shutdown waits for a publisher
// cycle to notice cancellation and return.
const outboxShutdownGrace = 20 * time.Second

type outboxPublisher struct {
	store    *driftdb.Store
	queue    sqs.Queue
	queueURL string
	log      zerolog.Logger
}

// runOutboxPublisher starts the report outbox publisher. It runs in the API
// process so publishing no longer depends on the request that created the
// report.
func (a *App) runOutboxPublisher(ctx context.Context) {
	settings := a.cfg.Outbox

	a.log.Info().
		Dur("interval", settings.Interval).
		Int("batch_size", settings.BatchSize).
		Int("concurrency", settings.Concurrency).
		Dur("send_timeout", settings.SendTimeout).
		Dur("claim_lease", settings.ClaimLease).
		Int("max_attempts", settings.MaxAttempts).
		Msg("outbox publisher started")

	publisher := &outboxPublisher{
		store:    a.Store,
		queue:    a.awsSQS,
		queueURL: a.queueURL,
		log:      a.log,
	}
	publisher.run(ctx, settings)
}

// run never starts a cycle while the previous one is still running. This keeps
// the number of database connections and in-flight SQS calls bounded even when
// the configured interval is shorter than the time a batch takes to publish.
func (p *outboxPublisher) run(ctx context.Context, settings config.Outbox) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	consecutiveAllFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			successes, failures := p.publishOutbox(ctx, settings)
			if ctx.Err() != nil {
				return
			}

			if failures > 0 && successes == 0 {
				consecutiveAllFailures++
			} else {
				consecutiveAllFailures = 0
			}

			delay := settings.Interval
			if consecutiveAllFailures >= settings.FailureThreshold {
				delay = settings.Cooldown
				p.log.Warn().
					Int("consecutive_all_failure_cycles", consecutiveAllFailures).
					Int("failures", failures).
					Dur("cooldown", settings.Cooldown).
					Msg("outbox: pausing after repeated cycles where every send failed")
			}
			timer.Reset(delay)
		}
	}
}

// publishOutbox claims a batch in one short statement, then sends the entries
// outside any database transaction. The claim token is the fencing token used
// for the final compare-and-set update.
func (p *outboxPublisher) publishOutbox(ctx context.Context, settings config.Outbox) (successes, failures int) {
	entries, err := p.store.OutboxStore.ClaimPendingReports(ctx, settings.BatchSize, settings.ClaimLease)
	if err != nil {
		if ctx.Err() == nil {
			p.log.Error().Err(err).Msg("outbox: claim pending entries")
		}
		return 0, 0
	}
	if len(entries) == 0 {
		return 0, 0
	}
	if settings.Concurrency <= 0 {
		p.log.Error().Msg("outbox: concurrency must be greater than zero")
		return 0, 0
	}

	var (
		wg                      sync.WaitGroup
		successCount, failCount atomic.Int64
	)
	jobs := make(chan driftdb.OutboxEntry)

	for i := 0; i < settings.Concurrency; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for entry := range jobs {
				if ctx.Err() != nil {
					return
				}

				if err := p.publishOutboxEntry(ctx, settings, entry); err != nil {
					failCount.Add(1)
					continue
				}
				successCount.Add(1)
			}
		}()
	}

claimLoop:
	for _, entry := range entries {
		select {
		case jobs <- entry:
		case <-ctx.Done():
			break claimLoop
		}
	}

	close(jobs)
	wg.Wait()

	return int(successCount.Load()), int(failCount.Load())
}
func (p *outboxPublisher) publishOutboxEntry(ctx context.Context, settings config.Outbox, entry driftdb.OutboxEntry) error {
	sendCtx, cancelSend := context.WithTimeout(ctx, settings.SendTimeout)
	_, sendErr := p.queue.SendMessage(sendCtx, p.queueURL, job.Report{
		ID:     entry.ReportID,
		UserID: entry.UserID,
		Type:   entry.ReportType,
	}, nil)
	cancelSend()

	if sendErr != nil {
		return p.handleOutboxSendFailure(ctx, settings, entry, sendErr)
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), config.OutboxCleanupTimeout)
	defer cancelCleanup()

	if err := p.store.OutboxStore.MarkPublished(cleanupCtx, entry.ID, entry.ClaimToken); err != nil {
		p.log.Error().
			Err(err).
			Str("outbox_id", entry.ID.String()).
			Str("report_id", entry.ReportID.String()).
			Msg("outbox: message was sent but could not be marked published")
		return err
	}

	// The SQS send already happened. MarkPublished is conditional on the claim
	// token so a stale publisher cannot overwrite a newer claim. If it fails, the
	// lease allows another publisher to retry, which may duplicate the send.
	p.log.Info().
		Str("outbox_id", entry.ID.String()).
		Str("report_id", entry.ReportID.String()).
		Msg("outbox: message published")

	return nil
}

func (p *outboxPublisher) handleOutboxSendFailure(
	ctx context.Context,
	settings config.Outbox,
	entry driftdb.OutboxEntry,
	sendErr error,
) error {
	class := sqs.ClassifySendError(sendErr)

	// Only a failure caused by this payload consumes the per-entry retry budget.
	// A regional SQS outage must not dead-letter every report in the outbox.
	countFailure := class == sqs.SendErrorPermanentEntry
	sendFailures := entry.SendFailures
	if countFailure {
		sendFailures++
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), config.OutboxCleanupTimeout)
	defer cancelCleanup()

	if class == sqs.SendErrorPermanentEntry && sendFailures >= settings.MaxAttempts {
		if err := p.store.OutboxStore.MarkDead(
			cleanupCtx,
			entry.ID,
			entry.ClaimToken,
			sendErr.Error(),
		); err != nil {
			p.log.Error().
				Err(err).
				Str("outbox_id", entry.ID.String()).
				Str("report_id", entry.ReportID.String()).
				Msg("outbox: could not mark entry dead")
			return err
		}

		p.log.Error().
			Err(sendErr).
			Str("outbox_id", entry.ID.String()).
			Str("report_id", entry.ReportID.String()).
			Int("send_failures", sendFailures).
			Msg("outbox: entry moved to dead state")
		return sendErr
	}

	nextAttempt := time.Now().Add(retryDelay(settings.RetryBase, settings.RetryMax, entry.Attempts))
	if err := p.store.OutboxStore.ReleaseClaim(
		cleanupCtx,
		entry.ID,
		entry.ClaimToken,
		nextAttempt,
		sendErr.Error(),
		countFailure,
	); err != nil {
		p.log.Error().
			Err(err).
			Str("outbox_id", entry.ID.String()).
			Str("report_id", entry.ReportID.String()).
			Msg("outbox: could not release claim after send failure")
		return err
	}

	p.log.Warn().
		Err(sendErr).
		Str("outbox_id", entry.ID.String()).
		Str("report_id", entry.ReportID.String()).
		Str("error_class", class.String()).
		Int("attempts", entry.Attempts).
		Int("send_failures", sendFailures).
		Time("next_attempt_at", nextAttempt).
		Msg("outbox: send failed; retry scheduled")

	return sendErr
}

// retryDelay uses the claim attempt count so the delay grows across repeated
// claims, even when a global outage does not increment send_failures. Jitter
// prevents all API instances from retrying at the same instant.
func retryDelay(base, maxDelay time.Duration, attempts int) time.Duration {
	if base <= 0 {
		return 0
	}
	if maxDelay < base {
		maxDelay = base
	}
	if attempts < 1 {
		attempts = 1
	}

	delay := base
	for i := 1; i < attempts && delay < maxDelay; i++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}

	jittered := time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
	if jittered > maxDelay {
		return maxDelay
	}

	return jittered
}
