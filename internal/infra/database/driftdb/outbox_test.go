package driftdb

import (
	"context"
	"testing"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pendingEntry looks up one entry among the unpublished ones, so tests do not
// depend on what earlier tests left in the shared table.
func pendingEntry(t *testing.T, id uuid.UUID) (OutboxEntry, bool) {
	t.Helper()

	pending, err := testOutboxDB.Pending(context.Background(), 100)
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

func TestOutboxDB_PendingAndMarkPublished(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	pending, ok := pendingEntry(t, entry.ID)
	require.True(t, ok, "an enqueued entry must be pending")
	assert.Equal(t, entry.UserID, pending.UserID)
	assert.Equal(t, entry.ReportID, pending.ReportID)
	assert.Equal(t, entry.ReportType, pending.ReportType)

	require.NoError(t, testOutboxDB.MarkPublished(context.Background(), entry.ID))
	// Publishing twice must not fail.
	require.NoError(t, testOutboxDB.MarkPublished(context.Background(), entry.ID))

	_, ok = pendingEntry(t, entry.ID)
	assert.False(t, ok, "a published entry must not be returned again")
}

func TestOutboxDB_PendingRespectsLimit(t *testing.T) {
	user := createTestUser(t)

	for i := 0; i < 3; i++ {
		report := createTestReport(t, user.ID)
		createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)
	}

	pending, err := testOutboxDB.Pending(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
}

func TestOutboxDB_DeletedWithReport(t *testing.T) {
	user := createTestUser(t)
	report := createTestReport(t, user.ID)
	entry := createTestOutboxEntry(t, user.ID, report.ID, report.ReportType)

	require.NoError(t, testReportDB.Delete(context.Background(), user.ID, report.ID))

	_, ok := pendingEntry(t, entry.ID)
	assert.False(t, ok, "deleting a report must remove its publication intent")
}

func TestStore_WithTX_CommitsReportAndOutbox(t *testing.T) {
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
