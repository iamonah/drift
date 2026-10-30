package main

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/aws/smithy-go"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/sqs"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryDelayUsesAttemptsAndCapsAtMax(t *testing.T) {
	base := 5 * time.Second
	max := 5 * time.Minute

	first := retryDelay(base, max, 0)
	assert.GreaterOrEqual(t, first, base*4/5)
	assert.LessOrEqual(t, first, base*6/5)

	later := retryDelay(base, max, 8)
	assert.Greater(t, later, first)
	assert.LessOrEqual(t, later, max)

	capped := retryDelay(base, max, 100)
	assert.GreaterOrEqual(t, capped, max*4/5)
	assert.LessOrEqual(t, capped, max)
}

func TestRetryDelayTreatsMaxBelowBaseAsBase(t *testing.T) {
	delay := retryDelay(10*time.Second, 5*time.Second, 1)
	assert.GreaterOrEqual(t, delay, 8*time.Second)
	assert.LessOrEqual(t, delay, 10*time.Second)
}

type publishedRecord struct {
	id    uuid.UUID
	token uuid.UUID
}

type releaseRecord struct {
	id           uuid.UUID
	token        uuid.UUID
	nextAttempt  time.Time
	errorMessage string
	countFailure bool
}

type deadRecord struct {
	id           uuid.UUID
	token        uuid.UUID
	errorMessage string
}

type fakeOutboxStore struct {
	mu        sync.Mutex
	claimable []driftdb.OutboxEntry
	claimErr  error

	published []publishedRecord
	released  []releaseRecord
	dead      []deadRecord

	publishErr error
	releaseErr error
	deadErr    error
}

func (f *fakeOutboxStore) Enqueue(context.Context, driftdb.OutboxEntry) error {
	return nil
}

func (f *fakeOutboxStore) ClaimPendingReports(_ context.Context, limit int, _ time.Duration) ([]driftdb.OutboxEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if limit <= 0 || len(f.claimable) == 0 {
		return nil, nil
	}

	count := limit
	if count > len(f.claimable) {
		count = len(f.claimable)
	}
	entries := f.claimable[:count]
	f.claimable = f.claimable[count:]

	return entries, nil
}

func (f *fakeOutboxStore) MarkPublished(_ context.Context, id uuid.UUID, token uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, publishedRecord{id: id, token: token})
	return nil
}

func (f *fakeOutboxStore) ReleaseClaim(
	_ context.Context,
	id uuid.UUID,
	token uuid.UUID,
	nextAttempt time.Time,
	errorMessage string,
	countFailure bool,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.released = append(f.released, releaseRecord{
		id:           id,
		token:        token,
		nextAttempt:  nextAttempt,
		errorMessage: errorMessage,
		countFailure: countFailure,
	})
	return nil
}

func (f *fakeOutboxStore) MarkDead(_ context.Context, id uuid.UUID, token uuid.UUID, errorMessage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.deadErr != nil {
		return f.deadErr
	}
	f.dead = append(f.dead, deadRecord{id: id, token: token, errorMessage: errorMessage})
	return nil
}

type fakeOutboxQueue struct {
	mu          sync.Mutex
	err         error
	delay       time.Duration
	sent        int
	inFlight    int
	maxInFlight int
}

func (f *fakeOutboxQueue) GetQueueURL(context.Context, string) (string, error) {
	return "test-queue", nil
}

func (f *fakeOutboxQueue) ReceiveMessages(context.Context, string, int32, int32) ([]sqs.Message, error) {
	return nil, nil
}

func (f *fakeOutboxQueue) DeleteMessage(context.Context, string, string) error {
	return nil
}

func (f *fakeOutboxQueue) ChangeMessageVisibility(context.Context, string, string, int32) error {
	return nil
}

func (f *fakeOutboxQueue) SendMessage(ctx context.Context, _ string, _ any, _ *sqs.SendOptions) (string, error) {
	f.mu.Lock()
	f.sent++
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if f.delay > 0 {
		timer := time.NewTimer(f.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
	}

	if f.err != nil {
		return "", f.err
	}
	return "message-id", nil
}

func (f *fakeOutboxQueue) snapshot() (sent, maxInFlight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent, f.maxInFlight
}

func fakePublisher(store *fakeOutboxStore, queue *fakeOutboxQueue) *outboxPublisher {
	return &outboxPublisher{
		store:    &driftdb.Store{OutboxStore: store},
		queue:    queue,
		queueURL: "test-queue",
		log:      zerolog.Nop(),
	}
}

func fakeOutboxSettings() config.Outbox {
	return config.Outbox{
		Interval:         time.Second,
		BatchSize:        10,
		Concurrency:      3,
		SendTimeout:      time.Second,
		ClaimLease:       time.Minute,
		MaxAttempts:      3,
		RetryBase:        time.Millisecond,
		RetryMax:         time.Second,
		FailureThreshold: 2,
		Cooldown:         time.Second,
	}
}

func fakeClaimedEntry() driftdb.OutboxEntry {
	return driftdb.OutboxEntry{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		ReportID:   uuid.New(),
		ReportType: "monsters",
		ClaimToken: uuid.New(),
		Attempts:   1,
	}
}

func TestOutboxPublisherPublishesClaimedEntry(t *testing.T) {
	entry := fakeClaimedEntry()
	store := &fakeOutboxStore{claimable: []driftdb.OutboxEntry{entry}}
	queue := &fakeOutboxQueue{}
	publisher := fakePublisher(store, queue)

	successes, failures := publisher.publishOutbox(context.Background(), fakeOutboxSettings())

	assert.Equal(t, 1, successes)
	assert.Equal(t, 0, failures)
	require.Len(t, store.published, 1)
	assert.Equal(t, entry.ID, store.published[0].id)
	assert.Equal(t, entry.ClaimToken, store.published[0].token)
	assert.Empty(t, store.released)
	assert.Empty(t, store.dead)
	sent, _ := queue.snapshot()
	assert.Equal(t, 1, sent)
}

func TestOutboxPublisherTransientFailureDoesNotConsumeBudget(t *testing.T) {
	entry := fakeClaimedEntry()
	entry.SendFailures = 10
	store := &fakeOutboxStore{claimable: []driftdb.OutboxEntry{entry}}
	queue := &fakeOutboxQueue{err: &smithy.GenericAPIError{Code: "ServiceUnavailable"}}
	publisher := fakePublisher(store, queue)
	settings := fakeOutboxSettings()
	settings.MaxAttempts = 3

	successes, failures := publisher.publishOutbox(context.Background(), settings)

	assert.Equal(t, 0, successes)
	assert.Equal(t, 1, failures)
	assert.Empty(t, store.published)
	assert.Empty(t, store.dead)
	require.Len(t, store.released, 1)
	assert.Equal(t, entry.ID, store.released[0].id)
	assert.Equal(t, entry.ClaimToken, store.released[0].token)
	assert.False(t, store.released[0].countFailure)
	assert.True(t, store.released[0].nextAttempt.After(time.Now()))
}

func TestOutboxPublisherPermanentFailureCountsBudget(t *testing.T) {
	entry := fakeClaimedEntry()
	entry.SendFailures = 0
	store := &fakeOutboxStore{claimable: []driftdb.OutboxEntry{entry}}
	queue := &fakeOutboxQueue{err: &smithy.GenericAPIError{Code: "InvalidMessageContents"}}
	publisher := fakePublisher(store, queue)
	settings := fakeOutboxSettings()
	settings.MaxAttempts = 3

	successes, failures := publisher.publishOutbox(context.Background(), settings)

	assert.Equal(t, 0, successes)
	assert.Equal(t, 1, failures)
	assert.Empty(t, store.dead)
	require.Len(t, store.released, 1)
	assert.True(t, store.released[0].countFailure)
}

func TestOutboxPublisherPermanentFailureAtLimitMarksDead(t *testing.T) {
	entry := fakeClaimedEntry()
	entry.SendFailures = 2
	store := &fakeOutboxStore{claimable: []driftdb.OutboxEntry{entry}}
	queue := &fakeOutboxQueue{err: &smithy.GenericAPIError{Code: "MessageTooLong"}}
	publisher := fakePublisher(store, queue)
	settings := fakeOutboxSettings()
	settings.MaxAttempts = 3

	successes, failures := publisher.publishOutbox(context.Background(), settings)

	assert.Equal(t, 0, successes)
	assert.Equal(t, 1, failures)
	assert.Empty(t, store.released)
	require.Len(t, store.dead, 1)
	assert.Equal(t, entry.ID, store.dead[0].id)
	assert.Equal(t, entry.ClaimToken, store.dead[0].token)
}

func TestOutboxPublisherGlobalFailureNeverDeadLetters(t *testing.T) {
	entry := fakeClaimedEntry()
	entry.SendFailures = 100
	store := &fakeOutboxStore{claimable: []driftdb.OutboxEntry{entry}}
	queue := &fakeOutboxQueue{err: &smithy.GenericAPIError{Code: "AccessDenied"}}
	publisher := fakePublisher(store, queue)
	settings := fakeOutboxSettings()
	settings.MaxAttempts = 1

	successes, failures := publisher.publishOutbox(context.Background(), settings)

	assert.Equal(t, 0, successes)
	assert.Equal(t, 1, failures)
	assert.Empty(t, store.dead)
	require.Len(t, store.released, 1)
	assert.False(t, store.released[0].countFailure)
}

func TestOutboxPublisherBoundedConcurrency(t *testing.T) {
	settings := fakeOutboxSettings()
	settings.Concurrency = 3
	settings.BatchSize = 10

	entries := make([]driftdb.OutboxEntry, 10)
	for i := range entries {
		entries[i] = fakeClaimedEntry()
	}

	store := &fakeOutboxStore{claimable: entries}
	queue := &fakeOutboxQueue{delay: 10 * time.Millisecond}
	publisher := fakePublisher(store, queue)

	successes, failures := publisher.publishOutbox(context.Background(), settings)

	assert.Equal(t, 10, successes)
	assert.Equal(t, 0, failures)
	sent, maxInFlight := queue.snapshot()
	assert.Equal(t, 10, sent)
	assert.LessOrEqual(t, maxInFlight, settings.Concurrency)
	assert.Equal(t, 10, len(store.published))
}

func TestOutboxPublisherClaimErrorReturnsNoCounts(t *testing.T) {
	store := &fakeOutboxStore{claimErr: context.DeadlineExceeded}
	queue := &fakeOutboxQueue{}
	publisher := fakePublisher(store, queue)

	successes, failures := publisher.publishOutbox(context.Background(), fakeOutboxSettings())

	assert.Equal(t, 0, successes)
	assert.Equal(t, 0, failures)
	sent, _ := queue.snapshot()
	assert.Equal(t, 0, sent)
}

func TestOutboxPublisherPausesAfterRepeatedAllFailureCycles(t *testing.T) {
	entries := make([]driftdb.OutboxEntry, 20)
	for i := range entries {
		entries[i] = fakeClaimedEntry()
	}

	store := &fakeOutboxStore{claimable: entries}
	queue := &fakeOutboxQueue{err: &smithy.GenericAPIError{Code: "ServiceUnavailable"}}
	publisher := fakePublisher(store, queue)

	settings := fakeOutboxSettings()
	settings.Interval = time.Millisecond
	settings.Cooldown = 100 * time.Millisecond
	settings.FailureThreshold = 2
	settings.BatchSize = 1
	settings.Concurrency = 1

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	publisher.run(ctx, settings)

	sent, _ := queue.snapshot()
	assert.LessOrEqual(t, sent, 3, "publisher should pause instead of retrying every interval")
}
