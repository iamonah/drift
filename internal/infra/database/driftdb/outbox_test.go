package driftdb

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearOutbox(t *testing.T) {
	t.Helper()

	_, err := testDB.ExecContext(context.Background(), "TRUNCATE report_outbox")
	require.NoError(t, err)
}

// pendingEntry looks up one entry among the currently claimable ones, so tests
// do not depend on what earlier tests left in the shared table.
func pendingEntry(t *testing.T, id uuid.UUID) (OutboxEntry, bool) {
	t.Helper()

	pending, err := testOutboxDB.GetPendingReports(context.Background(), 1000)
	require.NoError(t, err)

	for _, entry := range pending {
		if entry.ID == id {
			return entry, true
		}
	}

	return OutboxEntry{}, false
}

func createTestOutboxEntry(t *testing.T, userID uuid.UUID, reportID uuid.UUID, reportType string) OutboxEntry {
	t.Helper()

	entry := OutboxEntry{
		ID:         uuid.New(),
		UserID:     userID,
		ReportID:   reportID,
		ReportType: reportType,
	}
	require.NoError(t, testOutboxDB.Enqueue(context.Background(), entry))

	return entry
}

type outboxState struct {
	ClaimToken   sql.NullString
	Attempts     int
	SendFailures int
	NextAttempt  time.Time
	LastError    sql.NullString
	DeadAt       sql.NullTime
	PublishedAt  sql.NullTime
}

func getOutboxState(t *testing.T, id uuid.UUID) outboxState {
	t.Helper()

	var state outboxState
	err := testDB.QueryRowContext(context.Background(), `
		SELECT claim_token, attempts, send_failures, next_attempt_at,
		       last_error, dead_at, published_at
		FROM report_outbox
		WHERE id = $1
	`, id).Scan(
		&state.ClaimToken,
		&state.Attempts,
		&state.SendFailures,
		&state.NextAttempt,
		&state.LastError,
		&state.DeadAt,
		&state.PublishedAt,
	)
	require.NoError(t, err)

	return state
}

func TestOutboxDB_PendingAndMarkPublished(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	pending, ok := pendingEntry(t, entry.ID)
	require.True(t, ok, "an enqueued entry must be pending")
	assert.Equal(t, entry.UserID, pending.UserID)
	assert.Equal(t, entry.ReportID, pending.ReportID)
	assert.Equal(t, entry.ReportType, pending.ReportType)

	claimed, ok := claimPendingByID(t, entry.ID)
	require.True(t, ok, "the pending entry must be claimable")
	assert.Equal(t, entry.UserID, claimed.UserID)
	assert.Equal(t, entry.ReportID, claimed.ReportID)
	assert.Equal(t, entry.ReportType, claimed.ReportType)
	assert.NotEqual(t, uuid.Nil(), claimed.ClaimToken)
	assert.Equal(t, 1, claimed.Attempts)
	assert.Equal(t, 0, claimed.SendFailures)

	require.NoError(t, testOutboxDB.MarkPublished(context.Background(), entry.ID, claimed.ClaimToken))
	// A retry after a lost response must be idempotent.
	require.NoError(t, testOutboxDB.MarkPublished(context.Background(), entry.ID, claimed.ClaimToken))

	_, ok = pendingEntry(t, entry.ID)
	assert.False(t, ok, "a published entry must not be returned again")

	state := getOutboxState(t, entry.ID)
	assert.True(t, state.PublishedAt.Valid)
	assert.False(t, state.DeadAt.Valid)
	assert.False(t, state.ClaimToken.Valid)
}

func TestOutboxDB_PendingRespectsLimit(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	for i := 0; i < 3; i++ {
		report := createTestReport(t, user.ID)
		createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)
	}

	pending, err := testOutboxDB.GetPendingReports(context.Background(), 2)
	require.NoError(t, err)
	assert.Len(t, pending, 2)

	pending, err = testOutboxDB.GetPendingReports(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, pending, 3)
}

func TestOutboxDB_DeletedWithReport(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	require.NoError(t, testReportDB.Delete(context.Background(), user.ID, report.ID))

	_, ok := pendingEntry(t, entry.ID)
	assert.False(t, ok, "deleting a report must remove its publication intent")
}

func TestOutboxDB_ClaimSkipsEntriesWithActiveLease(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	firstReport := createTestReport(t, user.ID)
	secondReport := createTestReport(t, user.ID)
	first := createTestOutboxEntry(t, user.ID, firstReport.ID, firstReport.ReportType)
	second := createTestOutboxEntry(t, user.ID, secondReport.ID, secondReport.ReportType)

	firstClaim, err := testOutboxDB.ClaimPendingReports(context.Background(), 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, firstClaim, 1)

	secondClaim, err := testOutboxDB.ClaimPendingReports(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, secondClaim, 1)
	assert.NotEqual(t, firstClaim[0].ID, secondClaim[0].ID)

	claimedIDs := map[uuid.UUID]bool{firstClaim[0].ID: true, secondClaim[0].ID: true}
	assert.True(t, claimedIDs[first.ID])
	assert.True(t, claimedIDs[second.ID])

	pending, err := testOutboxDB.GetPendingReports(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestOutboxDB_ReleaseClaimBackoffAndFailureAccounting(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	first, err := testOutboxDB.ClaimPendingReports(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)

	nextAttempt := time.Now().Add(time.Hour)
	require.NoError(t, testOutboxDB.ReleaseClaim(
		context.Background(),
		entry.ID,
		first[0].ClaimToken,
		nextAttempt,
		"temporary outage",
		false,
	))

	state := getOutboxState(t, entry.ID)
	assert.False(t, state.ClaimToken.Valid)
	assert.Equal(t, 1, state.Attempts)
	assert.Equal(t, 0, state.SendFailures, "a global failure must not consume the per-entry budget")
	assert.True(t, state.NextAttempt.After(time.Now()))
	assert.Equal(t, "temporary outage", state.LastError.String)

	// Make the row eligible immediately for the next claim.
	_, err = testDB.ExecContext(
		context.Background(),
		"UPDATE report_outbox SET next_attempt_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = $1",
		entry.ID,
	)
	require.NoError(t, err)

	second, err := testOutboxDB.ClaimPendingReports(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, 2, second[0].Attempts)

	require.NoError(t, testOutboxDB.ReleaseClaim(
		context.Background(),
		entry.ID,
		second[0].ClaimToken,
		time.Now().Add(time.Minute),
		"bad payload",
		true,
	))

	state = getOutboxState(t, entry.ID)
	assert.Equal(t, 1, state.SendFailures, "a permanent entry failure must consume the budget")
	assert.Equal(t, "bad payload", state.LastError.String)
}

func TestOutboxDB_MarkDeadAndReplay(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	claimed, err := testOutboxDB.ClaimPendingReports(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, testOutboxDB.MarkDead(
		context.Background(),
		entry.ID,
		claimed[0].ClaimToken,
		"message is too large",
	))

	state := getOutboxState(t, entry.ID)
	assert.True(t, state.DeadAt.Valid)
	assert.False(t, state.PublishedAt.Valid)
	assert.False(t, state.ClaimToken.Valid)
	assert.Equal(t, 1, state.SendFailures)
	assert.Equal(t, "message is too large", state.LastError.String)

	_, ok := pendingEntry(t, entry.ID)
	assert.False(t, ok, "a dead entry must not be claimable")

	require.NoError(t, testOutboxDB.ReplayDead(context.Background(), entry.ID))

	state = getOutboxState(t, entry.ID)
	assert.False(t, state.DeadAt.Valid)
	assert.Equal(t, 0, state.Attempts)
	assert.Equal(t, 0, state.SendFailures)
	assert.False(t, state.LastError.Valid)

	replayed, ok := pendingEntry(t, entry.ID)
	require.True(t, ok, "a replayed entry must become claimable")
	assert.Equal(t, 0, replayed.Attempts)

	require.ErrorIs(
		t,
		testOutboxDB.ReplayDead(context.Background(), entry.ID),
		ErrOutboxNotDead,
	)
}

func TestOutboxDB_OldClaimTokenCannotMarkPublished(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	first, err := testOutboxDB.ClaimPendingReports(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)

	require.NoError(t, testOutboxDB.ReleaseClaim(
		context.Background(),
		entry.ID,
		first[0].ClaimToken,
		time.Now().Add(-time.Second),
		"",
		false,
	))

	second, err := testOutboxDB.ClaimPendingReports(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.NotEqual(t, first[0].ClaimToken, second[0].ClaimToken)

	require.ErrorIs(
		t,
		testOutboxDB.MarkPublished(context.Background(), entry.ID, first[0].ClaimToken),
		ErrOutboxClaimLost,
	)
	require.NoError(t, testOutboxDB.MarkPublished(context.Background(), entry.ID, second[0].ClaimToken))
}

func TestOutboxDB_ConcurrentClaimsAreDisjoint(t *testing.T) {
	clearOutbox(t)

	const (
		entryCount  = 24
		workerCount = 4
	)

	user := createTestUser(t)
	ids := make(map[uuid.UUID]bool, entryCount)
	for i := 0; i < entryCount; i++ {
		report := createTestReport(t, user.ID)
		entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)
		ids[entry.ID] = true
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		seen  = make(map[uuid.UUID]int, entryCount)
		errCh = make(chan error, workerCount)
	)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				entries, err := testOutboxDB.ClaimPendingReports(context.Background(), 1, time.Minute)
				if err != nil {
					errCh <- err
					return
				}
				if len(entries) == 0 {
					return
				}

				mu.Lock()
				for _, entry := range entries {
					seen[entry.ID]++
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	require.Len(t, seen, entryCount)
	for id := range ids {
		assert.Equal(t, 1, seen[id], "each outbox entry must be claimed exactly once")
	}
}

func claimPendingByID(t *testing.T, id uuid.UUID) (OutboxEntry, bool) {
	t.Helper()

	entries, err := testOutboxDB.ClaimPendingReports(context.Background(), 1000, time.Minute)
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}

	return OutboxEntry{}, false
}

func TestStore_WithTX_CommitsReportAndOutbox(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)

	reportID := uuid.New()
	entryID := uuid.New()

	err := testStore.WithTX(context.Background(), func(store *Store) error {
		if _, err := store.ReportStore.CreateReport(context.Background(), Report{
			UserID:     user.ID,
			ID:         reportID,
			ReportType: ReportTypeMonsters.String(),
		}); err != nil {
			return err
		}

		return store.OutboxStore.Enqueue(context.Background(), OutboxEntry{
			ID:         entryID,
			UserID:     user.ID,
			ReportID:   reportID,
			ReportType: ReportTypeMonsters.String(),
		})
	})
	require.NoError(t, err)

	report, err := testReportDB.GetReportByUserID(context.Background(), user.ID, reportID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusPending, report.Status)

	pending, ok := pendingEntry(t, entryID)
	require.True(t, ok, "the publication must be committed with the report")
	assert.Equal(t, reportID, pending.ReportID)
}

func TestStore_WithTX_OutboxFailureRollsBackReport(t *testing.T) {
	clearOutbox(t)

	user := createTestUser(t)

	entryID := uuid.New()
	firstReportID := uuid.New()

	require.NoError(t, testStore.WithTX(context.Background(), func(store *Store) error {
		if _, err := store.ReportStore.CreateReport(context.Background(), Report{
			UserID:     user.ID,
			ID:         firstReportID,
			ReportType: ReportTypeMonsters.String(),
		}); err != nil {
			return err
		}

		return store.OutboxStore.Enqueue(context.Background(), OutboxEntry{
			ID:         entryID,
			UserID:     user.ID,
			ReportID:   firstReportID,
			ReportType: ReportTypeMonsters.String(),
		})
	}))

	// Reusing the outbox id violates its primary key, so this transaction must
	// fail as a whole rather than leave a report without a publication.
	secondReportID := uuid.New()
	err := testStore.WithTX(context.Background(), func(store *Store) error {
		if _, err := store.ReportStore.CreateReport(context.Background(), Report{
			UserID:     user.ID,
			ID:         secondReportID,
			ReportType: ReportTypeMonsters.String(),
		}); err != nil {
			return err
		}

		return store.OutboxStore.Enqueue(context.Background(), OutboxEntry{
			ID:         entryID,
			UserID:     user.ID,
			ReportID:   secondReportID,
			ReportType: ReportTypeMonsters.String(),
		})
	})
	require.Error(t, err)

	_, err = testReportDB.GetReportByUserID(context.Background(), user.ID, secondReportID)
	assert.ErrorIs(t, err, ErrReportNotFound, "the failed outbox insert must roll back the report")

	first, ok := pendingEntry(t, entryID)
	require.True(t, ok, "the first publication must survive")
	assert.Equal(t, firstReportID, first.ReportID)
}
