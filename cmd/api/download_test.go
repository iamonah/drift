package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/gorilla/mux"
	midd "github.com/iamonah/drift/cmd/api/mid"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/s3"
	"github.com/iamonah/drift/internal/util"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type fakeReportStore struct {
	report      driftdb.Report
	err         error
	gotUserID   uuid.UUID
	gotReportID uuid.UUID
}

func (f *fakeReportStore) CreateReport(context.Context, driftdb.Report) (driftdb.Report, error) {
	return driftdb.Report{}, nil
}

func (f *fakeReportStore) GetReportByUserID(_ context.Context, userID, id uuid.UUID) (driftdb.Report, error) {
	f.gotUserID = userID
	f.gotReportID = id

	if f.err != nil {
		return driftdb.Report{}, f.err
	}
	if f.report.UserID != userID || f.report.ID != id {
		return driftdb.Report{}, driftdb.ErrReportNotFound
	}

	return f.report, nil
}

func (f *fakeReportStore) ClaimReport(context.Context, uuid.UUID, uuid.UUID, time.Duration) (*driftdb.Report, error) {
	return nil, nil
}

func (f *fakeReportStore) MarkFailed(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeReportStore) MarkCompleted(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeReportStore) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeReportStore) UpdateLease(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Duration) error {
	return nil
}

type fakeObjectStorage struct {
	url           string
	err           error
	presignCalls  int
	gotBucket     string
	gotKey        string
	gotExpiration time.Duration
}

func (f *fakeObjectStorage) UploadObject(context.Context, string, string, []byte, string) error {
	return nil
}

func (f *fakeObjectStorage) HeadObject(context.Context, string, string) (s3.ObjectInfo, error) {
	return s3.ObjectInfo{}, nil
}

func (f *fakeObjectStorage) DeleteObject(context.Context, string, string) error {
	return nil
}

func (f *fakeObjectStorage) PresignGetObject(_ context.Context, bucketName, objectKey string, expiration time.Duration) (string, error) {
	f.presignCalls++
	f.gotBucket = bucketName
	f.gotKey = objectKey
	f.gotExpiration = expiration

	if f.err != nil {
		return "", f.err
	}

	return f.url, nil
}

func newDownloadTestApp(store *fakeReportStore, storage *fakeObjectStorage, expiration string) *App {
	return &App{
		cfg: &config.Config{
			AWSS3: config.AWSS3{
				BucketName:        "reports-bucket",
				PresignExpiration: expiration,
			},
		},
		Store:         &driftdb.Store{ReportStore: store},
		ObjectStorage: storage,
	}
}

func newDownloadRequest(userID, reportID uuid.UUID, logs *bytes.Buffer) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+reportID.String()+"/download", nil)
	r = mux.SetURLVars(r, map[string]string{"report_id": reportID.String()})

	ctx := context.WithValue(r.Context(), midd.AuthContextPayloadKey, &util.Payload{UserID: userID})
	logger := zerolog.New(logs)

	return r.WithContext(logger.WithContext(ctx))
}

func completedReport(userID, reportID uuid.UUID) driftdb.Report {
	return driftdb.Report{
		UserID: userID,
		ID:     reportID,
		Status: driftdb.ReportStatusCompleted,
		OutputObjectKey: sql.NullString{
			String: "users/" + userID.String() + "/reports/" + reportID.String() + ".csv.gz",
			Valid:  true,
		},
	}
}

func TestDownloadReportReturnsPresignedURL(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	reportID := uuid.New()
	report := completedReport(userID, reportID)
	store := &fakeReportStore{report: report}
	storage := &fakeObjectStorage{url: "http://localhost:4566/reports-bucket/report.csv.gz?X-Amz-Signature=test"}
	var logs bytes.Buffer

	rec := httptest.NewRecorder()
	newDownloadTestApp(store, storage, "5m").DownloadReport()(rec, newDownloadRequest(userID, reportID, &logs))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, userID, store.gotUserID)
	require.Equal(t, reportID, store.gotReportID)
	require.Equal(t, 1, storage.presignCalls)
	require.Equal(t, "reports-bucket", storage.gotBucket)
	require.Equal(t, report.OutputObjectKey.String, storage.gotKey)
	require.Equal(t, 5*time.Minute, storage.gotExpiration)

	var body reportDownloadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, storage.url, body.URL)
	require.Empty(t, logs.String())
}

func TestDownloadReportRejectsAnotherUsersReport(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	otherUserID := uuid.New()
	reportID := uuid.New()
	store := &fakeReportStore{report: completedReport(ownerID, reportID)}
	storage := &fakeObjectStorage{url: "http://localhost:4566/should-not-be-used"}
	var logs bytes.Buffer

	rec := httptest.NewRecorder()
	newDownloadTestApp(store, storage, "5m").DownloadReport()(rec, newDownloadRequest(otherUserID, reportID, &logs))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, otherUserID, store.gotUserID)
	require.Equal(t, reportID, store.gotReportID)
	require.Zero(t, storage.presignCalls)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "report not found", body["message"])
	require.Empty(t, logs.String())
}

func TestDownloadReportRequiresCompletedReportWithOutputKey(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	reportID := uuid.New()

	tests := []struct {
		name   string
		status driftdb.ReportStatus
		key    sql.NullString
	}{
		{
			name:   "pending report",
			status: driftdb.ReportStatusPending,
			key:    sql.NullString{String: "users/user/reports/report.csv.gz", Valid: true},
		},
		{
			name:   "processing report",
			status: driftdb.ReportStatusProcessing,
			key:    sql.NullString{String: "users/user/reports/report.csv.gz", Valid: true},
		},
		{
			name:   "failed report",
			status: driftdb.ReportStatusFailed,
			key:    sql.NullString{String: "users/user/reports/report.csv.gz", Valid: true},
		},
		{
			name:   "completed without output key",
			status: driftdb.ReportStatusCompleted,
			key:    sql.NullString{},
		},
		{
			name:   "completed with empty output key",
			status: driftdb.ReportStatusCompleted,
			key:    sql.NullString{Valid: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeReportStore{report: driftdb.Report{
				UserID:          userID,
				ID:              reportID,
				Status:          test.status,
				OutputObjectKey: test.key,
			}}
			storage := &fakeObjectStorage{}
			var logs bytes.Buffer

			rec := httptest.NewRecorder()
			newDownloadTestApp(store, storage, "5m").DownloadReport()(rec, newDownloadRequest(userID, reportID, &logs))

			require.Equal(t, http.StatusConflict, rec.Code)
			require.Zero(t, storage.presignCalls)

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, "report is not ready", body["message"])
			require.Empty(t, logs.String())
		})
	}
}

func TestDownloadReportMissingReport(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	reportID := uuid.New()
	store := &fakeReportStore{err: driftdb.ErrReportNotFound}
	storage := &fakeObjectStorage{}
	var logs bytes.Buffer

	rec := httptest.NewRecorder()
	newDownloadTestApp(store, storage, "5m").DownloadReport()(rec, newDownloadRequest(userID, reportID, &logs))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Zero(t, storage.presignCalls)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "report not found", body["message"])
}

func TestDownloadReportPresignFailure(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	reportID := uuid.New()
	store := &fakeReportStore{report: completedReport(userID, reportID)}
	storage := &fakeObjectStorage{err: errors.New("s3 unavailable")}
	var logs bytes.Buffer

	rec := httptest.NewRecorder()
	newDownloadTestApp(store, storage, "5m").DownloadReport()(rec, newDownloadRequest(userID, reportID, &logs))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, 1, storage.presignCalls)
	require.NotContains(t, rec.Body.String(), "s3 unavailable")
	require.Equal(t, 1, strings.Count(logs.String(), "request failed"))
	require.Contains(t, logs.String(), "s3 unavailable")
}
