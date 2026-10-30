// Package retry provides small, context-aware exponential backoff for
// dependency calls. It deliberately does not manage queues or persistent
// retries; callers decide what a permanent failure means.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/rs/zerolog"
)

// ErrRetriesExhausted identifies an operation that failed after its retry
// budget was used.
var ErrRetriesExhausted = errors.New("retry attempts exhausted")

const jitterRange = 0.2

// Policy controls an exponential backoff sequence.
//
// MaxRetries is the number of retries after the initial attempt. A zero value
// makes a single attempt with no delay.
type Policy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxRetries   int
}

// Permanent marks an error as one that should not be retried.
func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return &permanentError{err: err}
}

// IsPermanent reports whether err was marked by Permanent. It follows wrapped
// errors, so callers can add context without losing the classification.
func IsPermanent(err error) bool {
	var permanent *permanentError
	return errors.As(err, &permanent)
}

type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

// Delay returns the jittered delay before retry attempt. Attempt is 1-based.
//
// Jitter must be in [0, 1], where 0.5 applies no jitter. Values outside that
// range are clamped. The result is never greater than MaxDelay.
func (p Policy) Delay(attempt int, jitter float64) time.Duration {
	if p.InitialDelay <= 0 {
		return 0
	}
	if attempt < 1 {
		attempt = 1
	}

	maxDelay := p.MaxDelay
	if maxDelay < p.InitialDelay {
		maxDelay = p.InitialDelay
	}

	delay := p.InitialDelay
	for i := 1; i < attempt && delay < maxDelay; i++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}

	jitter = min(max(jitter, 0), 1)
	jittered := time.Duration(float64(delay) * (1 - jitterRange + jitter*2*jitterRange))
	if jittered > maxDelay {
		return maxDelay
	}

	return jittered
}

// Do runs fn until it succeeds, returns a permanent error, uses its retry
// budget, or the context is cancelled. Failed dependency calls are logged with
// the retry delay so operators can see why processing is waiting.
func Do(
	ctx context.Context,
	policy Policy,
	log zerolog.Logger,
	operation string,
	fn func(context.Context) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	maxRetries := max(policy.MaxRetries, 0)
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if IsPermanent(lastErr) {
			return lastErr
		}
		if attempt == maxRetries {
			break
		}

		delay := policy.Delay(attempt+1, rand.Float64())
		log.Warn().
			Err(lastErr).
			Str("operation", operation).
			Int("attempt", attempt+1).
			Int("max_attempts", maxRetries+1).
			Dur("retry_in", delay).
			Msg("dependency call failed; retrying")

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	log.Error().
		Err(lastErr).
		Str("operation", operation).
		Int("attempts", maxRetries+1).
		Msg("dependency retry budget exhausted")

	return fmt.Errorf("%s: %w after %d attempts: %w", operation, ErrRetriesExhausted, maxRetries+1, lastErr)
}
