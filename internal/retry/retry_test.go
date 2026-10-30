package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyDelayExponential(t *testing.T) {
	policy := Policy{
		InitialDelay: time.Second,
		MaxDelay:     30 * time.Second,
	}

	// A jitter of 0.5 leaves the exponential value unchanged.
	assert.Equal(t, time.Second, policy.Delay(1, 0.5))
	assert.Equal(t, 2*time.Second, policy.Delay(2, 0.5))
	assert.Equal(t, 4*time.Second, policy.Delay(3, 0.5))
	assert.Equal(t, 8*time.Second, policy.Delay(4, 0.5))
	assert.Equal(t, 16*time.Second, policy.Delay(5, 0.5))
	assert.Equal(t, 30*time.Second, policy.Delay(6, 0.5))
	assert.Equal(t, 30*time.Second, policy.Delay(100, 0.5))
}

func TestPolicyDelayJitterBounds(t *testing.T) {
	policy := Policy{
		InitialDelay: 10 * time.Second,
		MaxDelay:     20 * time.Second,
	}

	assert.Equal(t, 8*time.Second, policy.Delay(1, 0))
	assert.Equal(t, 12*time.Second, policy.Delay(1, 1))
	assert.Equal(t, 8*time.Second, policy.Delay(1, -1), "jitter below zero is clamped")
	assert.Equal(t, 12*time.Second, policy.Delay(1, 2), "jitter above one is clamped")
}

func TestPolicyDelayNeverExceedsMax(t *testing.T) {
	policy := Policy{
		InitialDelay: time.Second,
		MaxDelay:     5 * time.Second,
	}

	for _, jitter := range []float64{0, 0.5, 1} {
		for _, attempt := range []int{1, 2, 3, 10, 100} {
			assert.LessOrEqual(t, policy.Delay(attempt, jitter), policy.MaxDelay)
		}
	}
}

func TestDoRetriesTransientFailures(t *testing.T) {
	var calls int
	policy := Policy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetries: 3}

	err := Do(context.Background(), policy, zerolog.Nop(), "test call", func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestDoStopsOnPermanentFailure(t *testing.T) {
	var calls int
	policy := Policy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetries: 5}
	permanentErr := errors.New("invalid request")

	err := Do(context.Background(), policy, zerolog.Nop(), "test call", func(context.Context) error {
		calls++
		return Permanent(permanentErr)
	})

	require.Error(t, err)
	assert.True(t, IsPermanent(err))
	assert.ErrorIs(t, err, permanentErr)
	assert.Equal(t, 1, calls, "a permanent failure must not be retried")
}

func TestDoExhaustsRetryBudget(t *testing.T) {
	var calls int
	policy := Policy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetries: 2}
	transientErr := errors.New("service unavailable")

	err := Do(context.Background(), policy, zerolog.Nop(), "test call", func(context.Context) error {
		calls++
		return transientErr
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRetriesExhausted)
	assert.ErrorIs(t, err, transientErr)
	assert.False(t, IsPermanent(err))
	assert.Equal(t, 3, calls, "the initial attempt plus two retries")
}

func TestDoContextCancellationInterruptsWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel while Do is waiting between attempts. The one-hour delay makes the
	// test fail quickly if context cancellation does not interrupt the timer.
	time.AfterFunc(10*time.Millisecond, cancel)
	policy := Policy{InitialDelay: time.Hour, MaxDelay: time.Hour, MaxRetries: 3}

	start := time.Now()
	err := Do(ctx, policy, zerolog.Nop(), "test call", func(context.Context) error {
		return errors.New("temporary failure")
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), time.Second)
}

func TestDoDoesNotRunAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls int
	err := Do(ctx, Policy{}, zerolog.Nop(), "test call", func(context.Context) error {
		calls++
		return nil
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, calls)
}

func TestDoZeroRetriesMakesOneAttempt(t *testing.T) {
	var calls int
	err := Do(context.Background(), Policy{}, zerolog.Nop(), "test call", func(context.Context) error {
		calls++
		return errors.New("temporary failure")
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 1, calls)
}
