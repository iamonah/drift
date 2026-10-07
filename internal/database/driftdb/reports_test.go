package driftdb

import (
	"context"
	"testing"
	"time"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestReport(t *testing.T) Report {
	t.Helper()

	report := Report{
		UserID:     TestReportUser,
		ID:         uuid.New(),
		ReportType: "test",
	}

	err := testReportDB.CreateReport(context.Background(), report)
	require.NoError(t, err)

	return report
}

func TestReportDB_CreateReport(t *testing.T) {
	initTestData(t)

	report := Report{
		UserID:     TestReportUser,
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
	initTestData(t)

	report := createTestReport(t)

	err := testReportDB.CreateReport(context.Background(), report)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestReportDB_GetReport(t *testing.T) {
	initTestData(t)

	report := createTestReport(t)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.Equal(t, report.ID, retrieved.ID)
	assert.Equal(t, report.ReportType, retrieved.ReportType)
	assert.NotZero(t, retrieved.CreatedAt)
}

func TestReportDB_GetReport_NotFound(t *testing.T) {
	initTestData(t)

	_, err := testReportDB.GetReport(context.Background(), uuid.New(), uuid.New())
	assert.Error(t, err)
}

func TestReportDB_GetReportsByUser(t *testing.T) {
	initTestData(t)

	report1 := createTestReport(t)
	report2 := createTestReport(t)

	reports, err := testReportDB.GetReportsByUser(context.Background(), TestReportUser)
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
	initTestData(t)

	report := createTestReport(t)

	err := testReportDB.MarkStarted(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.StartedAt.Valid)
}

func TestReportDB_MarkFailed(t *testing.T) {
	initTestData(t)

	report := createTestReport(t)
	err := testReportDB.MarkFailed(context.Background(), report.UserID, report.ID, "file processing failed")
	require.NoError(t, err)

	retrieved, err := testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.FailedAt.Valid)
	assert.Equal(t, "file processing failed", retrieved.ErrorMessage.String)
}

func TestReportDB_MarkCompleted(t *testing.T) {
	initTestData(t)

	report := createTestReport(t)
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
	initTestData(t)

	report := createTestReport(t)

	err := testReportDB.Delete(context.Background(), report.UserID, report.ID)
	require.NoError(t, err)

	_, err = testReportDB.GetReport(context.Background(), report.UserID, report.ID)
	assert.Error(t, err)
}
