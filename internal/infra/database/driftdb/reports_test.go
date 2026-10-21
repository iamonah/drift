package driftdb

import (
	"context"
	"testing"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestReportDB_MarkStarted(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.MarkStarted(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReportByUserID(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.StartedAt.Valid)
	assert.Equal(t, ReportStatusProcessing, retrieved.Status)
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
