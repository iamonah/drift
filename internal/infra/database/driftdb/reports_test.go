package driftdb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClaimLease keeps the claim tests independent of the worker's own lease.
const testClaimLease = time.Minute

// expireLease moves a processing report's lease into the past so it becomes
// reclaimable without waiting.
func expireLease(t *testing.T, userID uuid.UUID, reportID uuid.UUID) {
	t.Helper()

	_, err := testDB.Exec(
		`UPDATE reports SET lease_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 minute' WHERE user_id = $1 AND id = $2`,
		userID, reportID,
	)
	require.NoError(t, err)
}

// setProcessingBudget overrides the attempt counters directly so tests can
// reach the exhaustion boundary without claiming a report repeatedly.
func setProcessingBudget(t *testing.T, userID uuid.UUID, reportID uuid.UUID, attempts, limit int) {
	t.Helper()

	_, err := testDB.Exec(
		`UPDATE reports SET processing_attempts = $3, processing_limit = $4 WHERE user_id = $1 AND id = $2`,
		userID, reportID, attempts, limit,
	)
	require.NoError(t, err)
}

func createTestReport(t *testing.T, userID uuid.UUID) Report {
	t.Helper()

	report, err := testReportDB.CreateReport(context.Background(), Report{
		UserID:     userID,
		ID:         uuid.New(),
		ReportType: ReportTypeMonsters.String(),
	})
	require.NoError(t, err)

	return report
}

func TestReportDB_CreateReport(t *testing.T) {
	user := createTestUser(t)

	created, err := testReportDB.CreateReport(context.Background(), Report{
		UserID:     user.ID,
		ID:         uuid.New(),
		ReportType: ReportTypeFoods.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, ReportStatusPending, created.Status)
	assert.NotZero(t, created.CreatedAt)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), created.UserID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, created.ReportType, retrieved.ReportType)
	assert.Equal(t, ReportStatusPending, retrieved.Status)
}

func TestReportDB_CreateReport_Duplicate(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	_, err := testReportDB.CreateReport(context.Background(), report)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestReportDB_CreateReport_InvalidType(t *testing.T) {
	user := createTestUser(t)

	_, err := testReportDB.CreateReport(context.Background(), Report{
		UserID:     user.ID,
		ID:         uuid.New(),
		ReportType: "banana",
	})
	assert.ErrorIs(t, err, ErrInvalidReportType)
}

func TestReportDB_GetReport(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, report.ID, retrieved.ID)
	assert.Equal(t, report.ReportType, retrieved.ReportType)
	assert.NotZero(t, retrieved.CreatedAt)
}

func TestReportDB_GetReport_NotFound(t *testing.T) {
	_, err := testReportDB.GetReportByUserID(context.Background(), uuid.New(), uuid.New())
	assert.Error(t, err)
}

func TestReportDB_GetReport_OtherUser(t *testing.T) {
	owner := createTestUser(t)
	other := createTestUser(t)
	report := createTestReport(t, owner.ID)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), other.ID, report.ID)

	assert.ErrorIs(t, err, ErrReportNotFound)
	assert.Zero(t, retrieved.ID)
}

// func TestReportDB_GetReportsByUser(t *testing.T) {
// 	user := createTestUser(t)
// 	report1 := createTestReport(t, user.ID)
// 	report2 := createTestReport(t, user.ID)

// 	reports, err := testReportDB.GetReportsByUser(context.Background(), user.ID)
// 	require.NoError(t, err)

// 	var found1, found2 bool
// 	for _, report := range reports {
// 		found1 = found1 || report.ID == report1.ID
// 		found2 = found2 || report.ID == report2.ID
// 	}

// 	assert.True(t, found1)
// 	assert.True(t, found2)
// }

func TestReportDB_ClaimReport(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, ReportStatusProcessing, claimed.Status)
	assert.True(t, claimed.StartedAt.Valid)
	assert.NotEqual(t, uuid.Nil(), claimed.ClaimToken)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusProcessing, retrieved.Status)
	assert.True(t, retrieved.StartedAt.Valid)
}

func TestReportDB_ClaimReport_AlreadyClaimed(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	assert.ErrorIs(t, err, ErrReportProcessing)
	assert.Nil(t, second, "a report must only be claimable once")
}

func TestReportDB_ClaimReport_StaleProcessing(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	// Simulate a worker that died mid-report, with its lease expired.
	expireLease(t, report.UserID, report.ID)

	reclaimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, reclaimed, "a stale processing report must be claimable again")
	assert.Equal(t, ReportStatusProcessing, reclaimed.Status)
	assert.NotEqual(t, uuid.Nil(), reclaimed.ClaimToken)
	assert.NotEqual(t, first.ClaimToken, reclaimed.ClaimToken, "a reclaim must take a new claim token")
}

func TestReportDB_ClaimReport_StartedAtDoesNotDriveLease(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	// An old start time with a live lease must still belong to its worker.
	_, err = testDB.Exec(
		`UPDATE reports SET started_at = CURRENT_TIMESTAMP - INTERVAL '1 hour' WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	)
	require.NoError(t, err)

	second, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	assert.ErrorIs(t, err, ErrReportProcessing)
	assert.Nil(t, second)
}

func TestReportDB_ClaimReport_NotFound(t *testing.T) {
	claimed, err := testReportDB.ClaimReport(context.Background(), uuid.New(), uuid.New(), testClaimLease)
	assert.ErrorIs(t, err, ErrReportNotFound)
	assert.Nil(t, claimed)
}

func TestReportDB_ClaimReport_TerminalReport(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	require.NoError(t, testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, claimed.ClaimToken, "users/test/reports/test.csv.gz"))

	again, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	assert.Nil(t, again, "a terminal report must not be claimed again")
}

func TestReportDB_ClaimReport_InvalidLease(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	for _, lease := range []time.Duration{0, -time.Minute} {
		claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, lease)
		assert.ErrorContains(t, err, "lease must be positive")
		assert.Nil(t, claimed)
	}

	// A rejected lease must not have touched the report.
	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusPending, retrieved.Status)
}

func TestReportDB_ClaimReport_ProcessingWithoutLease(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	// A processing report with no lease cannot be aged, so it must not be left
	// unclaimable forever.
	_, err = testDB.Exec(
		`UPDATE reports SET lease_expires_at = NULL WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	)
	require.NoError(t, err)

	reclaimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, reclaimed, "a processing report without a lease must be claimable")
	assert.True(t, reclaimed.StartedAt.Valid)
	assert.NotEqual(t, first.ClaimToken, reclaimed.ClaimToken)
}

func TestReportDB_ClaimReport_PendingAttemptsExhausted(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	setProcessingBudget(t, report.UserID, report.ID, 5, 5)

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	assert.Nil(t, claimed, "an exhausted pending report has no work left")

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusFailed, retrieved.Status, "an exhausted pending report must be terminalized")
	assert.True(t, retrieved.FailedAt.Valid)
	assert.Equal(t, reportAttemptsExhaustedMessage, retrieved.ErrorMessage.String)

	// A second claim must keep the report terminal rather than incrementing
	// attempts or returning it to processing.
	again, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	assert.Nil(t, again)

	var attempts int
	err = testDB.QueryRow(
		`SELECT processing_attempts FROM reports WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	).Scan(&attempts)
	require.NoError(t, err)
	assert.Equal(t, 5, attempts, "exhaustion must not consume another attempt")

	retrieved, err = testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusFailed, retrieved.Status)
}

func TestReportDB_ClaimReport_ExpiredProcessingAttemptsExhausted(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	setProcessingBudget(t, report.UserID, report.ID, 0, 1)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	// The only allowed attempt has now been consumed. Simulate the worker
	// dying without recording a terminal state.
	expireLease(t, report.UserID, report.ID)

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	assert.Nil(t, claimed, "an exhausted expired report has no work left")

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusFailed, retrieved.Status, "an exhausted expired report must be terminalized")
	assert.True(t, retrieved.FailedAt.Valid)
	assert.Equal(t, reportAttemptsExhaustedMessage, retrieved.ErrorMessage.String)

	// The worker that held the expired claim must not be able to overwrite the
	// newer terminal state.
	err = testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, first.ClaimToken, "stale.csv.gz")
	assert.ErrorIs(t, err, ErrReportOwnershipLost)
	err = testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, first.ClaimToken, "stale failure")
	assert.ErrorIs(t, err, ErrReportOwnershipLost)

	retrieved, err = testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusFailed, retrieved.Status)
}

func TestReportDB_ClaimReport_ExpiredProcessingWithAttemptsRemaining(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	setProcessingBudget(t, report.UserID, report.ID, 0, 3)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	expireLease(t, report.UserID, report.ID)

	second, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, second, "an expired report with attempts remaining must be reclaimable")
	assert.Equal(t, ReportStatusProcessing, second.Status)
	assert.NotEqual(t, first.ClaimToken, second.ClaimToken)

	var attempts int
	err = testDB.QueryRow(
		`SELECT processing_attempts FROM reports WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	).Scan(&attempts)
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestReportDB_ClaimReport_AttemptsExhaustedWithLiveLease(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	setProcessingBudget(t, report.UserID, report.ID, 0, 1)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	// The attempt budget is exhausted, but the worker still owns a live lease.
	// It must be allowed to finish rather than being terminalized underneath it.
	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	assert.ErrorIs(t, err, ErrReportProcessing)
	assert.Nil(t, claimed)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusProcessing, retrieved.Status)
	assert.False(t, retrieved.FailedAt.Valid)

	// Once that lease expires without a terminal state, the report becomes
	// terminal on the next claim.
	expireLease(t, report.UserID, report.ID)

	claimed, err = testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	assert.Nil(t, claimed)

	retrieved, err = testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusFailed, retrieved.Status)
}

func TestReportDB_ClaimReport_ConcurrentExhaustedTerminalization(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, report Report)
	}{
		{
			name: "pending",
			prepare: func(t *testing.T, report Report) {
				setProcessingBudget(t, report.UserID, report.ID, 5, 5)
			},
		},
		{
			name: "expired processing",
			prepare: func(t *testing.T, report Report) {
				setProcessingBudget(t, report.UserID, report.ID, 0, 1)
				claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
				require.NoError(t, err)
				require.NotNil(t, claimed)
				expireLease(t, report.UserID, report.ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := createTestUser(t)
			report := createTestReport(t, user.ID)
			tt.prepare(t, report)

			const workers = 8

			type claimResult struct {
				report *Report
				err    error
			}

			results := make(chan claimResult, workers)
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
					results <- claimResult{report: claimed, err: err}
				}()
			}
			wg.Wait()
			close(results)

			for res := range results {
				assert.NoError(t, res.err)
				assert.Nil(t, res.report, "no worker may claim an exhausted report")
			}

			retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
			require.NoError(t, err)
			assert.Equal(t, ReportStatusFailed, retrieved.Status)
			assert.True(t, retrieved.FailedAt.Valid)
		})
	}
}

func TestReportDB_MarkFailed(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	err = testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, claimed.ClaimToken, "file processing failed")
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.FailedAt.Valid)
	assert.Equal(t, ReportStatusFailed, retrieved.Status)
	assert.Equal(t, "file processing failed", retrieved.ErrorMessage.String)
}

// Failure requires the claim token of a processing attempt.
func TestReportDB_MarkFailed_RequiresClaim(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, uuid.New(), "failed to enqueue report")
	assert.ErrorIs(t, err, ErrReportOwnershipLost)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusPending, retrieved.Status)
}

func TestReportDB_MarkCompleted(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	objectKey := "users/test/reports/test.csv.gz"

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	err = testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, claimed.ClaimToken, objectKey)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.CompletedAt.Valid)
	assert.Equal(t, ReportStatusCompleted, retrieved.Status)
	assert.Equal(t, objectKey, retrieved.OutputObjectKey.String)
}

// Completion requires the claim token of a processing attempt.
func TestReportDB_MarkCompleted_RequiresClaim(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, uuid.New(), "users/test/reports/test.csv.gz")
	assert.ErrorIs(t, err, ErrReportOwnershipLost)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusPending, retrieved.Status)
}

func TestReportDB_UpdateLease(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	expireLease(t, report.UserID, report.ID)

	err = testReportDB.UpdateLease(context.Background(), report.UserID, report.ID, claimed.ClaimToken, testClaimLease)
	require.NoError(t, err)

	var expiresAt time.Time
	err = testDB.QueryRow(
		`SELECT lease_expires_at FROM reports WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	).Scan(&expiresAt)
	require.NoError(t, err)
	assert.True(t, expiresAt.After(time.Now().Add(30*time.Second)), "the lease must be extended")
}

func TestReportDB_UpdateLease_StaleToken(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	expireLease(t, report.UserID, report.ID)

	second, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, second)

	err = testReportDB.UpdateLease(context.Background(), report.UserID, report.ID, first.ClaimToken, testClaimLease)
	assert.ErrorIs(t, err, ErrReportOwnershipLost)

	require.NoError(t, testReportDB.UpdateLease(context.Background(), report.UserID, report.ID, second.ClaimToken, testClaimLease))
}

func TestReportDB_MarkCompleted_StaleToken(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	expireLease(t, report.UserID, report.ID)

	second, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, second)

	err = testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, first.ClaimToken, "stale.csv.gz")
	assert.ErrorIs(t, err, ErrReportOwnershipLost)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusProcessing, retrieved.Status)
	assert.False(t, retrieved.OutputObjectKey.Valid, "a stale worker must not write the output key")

	require.NoError(t, testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, second.ClaimToken, "current.csv.gz"))

	retrieved, err = testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusCompleted, retrieved.Status)
	assert.Equal(t, "current.csv.gz", retrieved.OutputObjectKey.String)
}

func TestReportDB_MarkFailed_StaleToken(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	expireLease(t, report.UserID, report.ID)

	second, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, second)

	err = testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, first.ClaimToken, "stale failure")
	assert.ErrorIs(t, err, ErrReportOwnershipLost)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusProcessing, retrieved.Status)
	assert.False(t, retrieved.ErrorMessage.Valid, "a stale worker must not write the error message")

	require.NoError(t, testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, second.ClaimToken, "current failure"))

	retrieved, err = testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, ReportStatusFailed, retrieved.Status)
	assert.Equal(t, "current failure", retrieved.ErrorMessage.String)
}

func TestReportDB_ClaimReport_ConcurrentClaims(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	type claimResult struct {
		report *Report
		err    error
	}

	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
			results <- claimResult{report: claimed, err: err}
		}()
	}
	wg.Wait()
	close(results)

	var claimed, rejected int
	for res := range results {
		switch {
		case res.err == nil && res.report != nil:
			claimed++
		case errors.Is(res.err, ErrReportProcessing) && res.report == nil:
			rejected++
		default:
			t.Errorf("unexpected claim result: report=%v err=%v", res.report, res.err)
		}
	}

	assert.Equal(t, 1, claimed, "only one worker may claim the report")
	assert.Equal(t, 1, rejected, "the other claim must see an active lease")
}

func TestReportDB_Delete(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.Delete(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)

	_, err = testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	assert.Error(t, err)
}

func TestCheckReportStatus(t *testing.T) {
	for _, status := range []string{"pending", "processing", "completed", "failed"} {
		got, err := CheckReportStatus(status)
		require.NoError(t, err)
		assert.Equal(t, ReportStatus(status), got)
	}

	_, err := CheckReportStatus("queued")
	assert.Error(t, err)
}

func TestCheckReportType(t *testing.T) {
	for _, reportType := range []string{"monsters", "foods"} {
		got, err := CheckReportType(reportType)
		require.NoError(t, err)
		assert.Equal(t, ReportType(reportType), got)
	}

	_, err := CheckReportType("banana")
	assert.Error(t, err)
}
