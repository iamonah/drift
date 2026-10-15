package driftdb

import (
	"context"
	"testing"
	"time"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestReport(t *testing.T, userID uuid.UUID) Report {
	t.Helper()

	report := Report{
		UserID:     userID,
		ID:         uuid.New(),
		ReportType: "test",
	}

	err := testReportDB.CreateReport(context.Background(), report)
	require.NoError(t, err)

	return report
}

func TestReportDB_CreateReport(t *testing.T) {
	user := createTestUser(t)

	report := Report{
		UserID:     user.ID,
		ID:         uuid.New(),
		ReportType: "sales_summary",
	}

	err := testReportDB.CreateReport(context.Background(), report)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, report.ID, retrieved.ID)
	assert.Equal(t, report.ReportType, retrieved.ReportType)
}

func TestReportDB_CreateReport_Duplicate(t *testing.T) {

	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.CreateReport(context.Background(), report)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestReportDB_GetReport(t *testing.T) {

	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, report.ID, retrieved.ID)
	assert.Equal(t, report.ReportType, retrieved.ReportType)
	assert.NotZero(t, retrieved.CreatedAt)
}

func TestReportDB_GetReport_NotFound(t *testing.T) {

	_, err := testReportDB.GetReport(context.Background(), uuid.New(), uuid.New())
	assert.Error(t, err)
}

func TestReportDB_GetReportsByUser(t *testing.T) {

	user := createTestUser(t)
	report1 := createTestReport(t, user.ID)
	report2 := createTestReport(t, user.ID)

	reports, err := testReportDB.GetReportsByUser(context.Background(), user.ID)
	require.NoError(t, err)

	var found1, found2 bool
	for _, report := range reports {
		found1 = found1 || report.ID == report1.ID
		found2 = found2 || report.ID == report2.ID
	}

	assert.True(t, found1)
	assert.True(t, found2)
}

func TestReportDB_MarkStarted(t *testing.T) {

	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.MarkStarted(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.StartedAt.Valid)
}

func TestReportDB_MarkFailed(t *testing.T) {

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	err := testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, "file processing failed")
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.FailedAt.Valid)
	assert.Equal(t, "file processing failed", retrieved.ErrorMessage.String)
}

func TestReportDB_MarkCompleted(t *testing.T) {

	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	filePath := "/reports/test.pdf"
	downloadURL := "https://example.com/download/test"
	expiresAt := time.Now().Add(24 * time.Hour)

	err := testReportDB.MarkCompleted(context.Background(), report.UserID, report.ID, filePath, downloadURL, expiresAt)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.CompletedAt.Valid)
	assert.Equal(t, filePath, retrieved.OutputFilePath.String)
	assert.Equal(t, downloadURL, retrieved.DownloadURL.String)

}

func TestReportDB_Delete(t *testing.T) {

	user := createTestUser(t)
	report := createTestReport(t, user.ID)

	err := testReportDB.Delete(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)

	_, err = testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	assert.Error(t, err)
}
