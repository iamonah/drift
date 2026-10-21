package driftdb

import (
	"context"
	"testing"
	"time"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClaimLease keeps the claim tests independent of the worker's own lease.
const testClaimLease = time.Minute

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

	// Simulate a worker that died mid-report, well past the test lease.
	_, err = testDB.Exec(
		`UPDATE reports SET started_at = CURRENT_TIMESTAMP - INTERVAL '2 minutes' WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	)
	require.NoError(t, err)

	reclaimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, reclaimed, "a stale processing report must be claimable again")
	assert.Equal(t, ReportStatusProcessing, reclaimed.Status)
}

func TestReportDB_ClaimReport_NotFound(t *testing.T) {
	claimed, err := testReportDB.ClaimReport(context.Background(), uuid.New(), uuid.New(), testClaimLease)
	require.NoError(t, err)
	assert.Nil(t, claimed)
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

func TestReportDB_ClaimReport_ProcessingWithoutStartedAt(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	first, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, first)

	// A processing report with no start time cannot be aged, so it must not be
	// left unclaimable forever.
	_, err = testDB.Exec(
		`UPDATE reports SET started_at = NULL WHERE user_id = $1 AND id = $2`,
		report.UserID, report.ID,
	)
	require.NoError(t, err)

	reclaimed, err := testReportDB.ClaimReport(context.Background(), report.UserID, report.ID, testClaimLease)
	require.NoError(t, err)
	require.NotNil(t, reclaimed, "a processing report without a start time must be claimable")
	assert.True(t, reclaimed.StartedAt.Valid)
}

func TestReportDB_MarkFailed(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, "file processing failed")
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.FailedAt.Valid)
	assert.Equal(t, ReportStatusFailed, retrieved.Status)
	assert.Equal(t, "file processing failed", retrieved.ErrorMessage.String)
}

func TestReportDB_MarkCompleted(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	objectKey := "reports/test.pdf"

	err := testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, objectKey)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.CompletedAt.Valid)
	assert.Equal(t, ReportStatusCompleted, retrieved.Status)
	assert.Equal(t, objectKey, retrieved.OutputObjectKey.String)
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
