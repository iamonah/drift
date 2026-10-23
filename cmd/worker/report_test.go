package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/client/loz"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/s3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type workerReportStore struct {
	report       *driftdb.Report
	completedKey string
}

func (f *workerReportStore) CreateReport(context.Context, driftdb.Report) (driftdb.Report, error) {
	return driftdb.Report{}, nil
}

func (f *workerReportStore) GetReportByUserID(context.Context, uuid.UUID, uuid.UUID) (driftdb.Report, error) {
	return driftdb.Report{}, nil
}

func (f *workerReportStore) ClaimReport(context.Context, uuid.UUID, uuid.UUID, time.Duration) (*driftdb.Report, error) {
	return f.report, nil
}

func (f *workerReportStore) MarkFailed(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *workerReportStore) MarkCompleted(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, outputObjectKey string) error {
	f.completedKey = outputObjectKey
	return nil
}

func (f *workerReportStore) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *workerReportStore) UpdateLease(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Duration) error {
	return nil
}

type workerObjectStorage struct {
	uploadCalls  int
	presignCalls int
	bucket       string
	key          string
	contentType  string
	content      []byte
}

func (f *workerObjectStorage) UploadObject(_ context.Context, bucketName, objectKey string, content []byte, contentType string) error {
	f.uploadCalls++
	f.bucket = bucketName
	f.key = objectKey
	f.contentType = contentType
	f.content = append([]byte(nil), content...)

	return nil
}

func (f *workerObjectStorage) HeadObject(context.Context, string, string) (s3.ObjectInfo, error) {
	return s3.ObjectInfo{}, nil
}

func (f *workerObjectStorage) DeleteObject(context.Context, string, string) error {
	return nil
}

func (f *workerObjectStorage) PresignGetObject(context.Context, string, string, time.Duration) (string, error) {
	f.presignCalls++
	return "", nil
}

func TestProcessReportUploadsGeneratedReportDirectlyToS3(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": 1, "name": "Bokoblin", "category": "monsters"},
			},
		})
	}))
	defer server.Close()

	userID := uuid.New()
	reportID := uuid.New()
	store := &workerReportStore{report: &driftdb.Report{
		UserID:     userID,
		ID:         reportID,
		ReportType: driftdb.ReportTypeMonsters.String(),
		ClaimToken: uuid.New(),
	}}
	storage := &workerObjectStorage{}
	worker := &Worker{
		cfg:           &config.Config{AWSS3: config.AWSS3{BucketName: "reports-bucket"}},
		ObjectStorage: storage,
		reportStore:   &driftdb.Store{ReportStore: store},
		loz:           loz.NewClient(server.URL, server.Client()),
		log:           zerolog.Nop(),
	}

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.NoError(t, err)

	require.Equal(t, 1, storage.uploadCalls)
	require.Equal(t, "reports-bucket", storage.bucket)
	require.Contains(t, storage.key, "users/"+userID.String()+"/reports/"+reportID.String())
	require.Equal(t, "application/gzip", storage.contentType)
	require.NotEmpty(t, storage.content)
	require.Equal(t, storage.key, store.completedKey)
	require.Zero(t, storage.presignCalls, "the worker must upload directly rather than using a presigned URL")
}
