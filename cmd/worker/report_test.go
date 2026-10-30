package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/client/loz"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/s3"
	"github.com/iamonah/drift/internal/retry"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workerReportStore struct {
	report        *driftdb.Report
	completedKey  string
	claims        int
	failed        int
	failedMessage string
	leaseUpdates  int32
}

func (f *workerReportStore) CreateReport(context.Context, driftdb.Report) (driftdb.Report, error) {
	return driftdb.Report{}, nil
}

func (f *workerReportStore) GetReportByUserID(context.Context, uuid.UUID, uuid.UUID) (driftdb.Report, error) {
	return driftdb.Report{}, nil
}

func (f *workerReportStore) ClaimReport(context.Context, uuid.UUID, uuid.UUID, time.Duration) (*driftdb.Report, error) {
	f.claims++
	return f.report, nil
}

func (f *workerReportStore) MarkFailed(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, errorMessage string) error {
	f.failed++
	f.failedMessage = errorMessage
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
	atomic.AddInt32(&f.leaseUpdates, 1)
	return nil
}

type workerObjectStorage struct {
	uploadCalls  int
	uploadErrs   []error
	presignCalls int
	bucket       string
	key          string
	contentType  string
	content      []byte
}

func (f *workerObjectStorage) UploadObject(_ context.Context, bucketName, objectKey string, content []byte, contentType string) error {
	f.uploadCalls++

	if len(f.uploadErrs) > 0 {
		err := f.uploadErrs[0]
		f.uploadErrs = f.uploadErrs[1:]
		if err != nil {
			return err
		}
	}

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

func newReportTestWorker(
	server *httptest.Server,
	store *workerReportStore,
	storage *workerObjectStorage,
	policy retry.Policy,
) *Worker {
	return &Worker{
		cfg:             &config.Config{AWSS3: config.AWSS3{BucketName: "reports-bucket"}},
		ObjectStorage:   storage,
		reportStore:     &driftdb.Store{ReportStore: store},
		loz:             loz.NewClient(server.URL, server.Client()),
		log:             zerolog.Nop(),
		dependencyRetry: policy,
	}
}

func writeMonstersJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": []map[string]any{
			{"id": 1, "name": "Bokoblin", "category": "monsters"},
		},
	})
}

func TestProcessReportRetriesTransientLozFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		writeMonstersJSON(w)
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
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   2,
	})

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.NoError(t, err)

	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, 1, store.claims, "dependency retries must not make new report claims")
	assert.Zero(t, store.failed)
	assert.NotEmpty(t, store.completedKey)
}

func TestProcessReportDoesNotRetryPermanentLozFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "invalid request", http.StatusBadRequest)
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
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   3,
	})

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.Error(t, err)

	assert.Equal(t, int32(1), calls.Load(), "a permanent Loz failure must not be retried")
	assert.Equal(t, 1, store.failed)
	assert.Empty(t, store.completedKey)
	assert.Zero(t, storage.uploadCalls)
}

func TestProcessReportExhaustedLozRetriesRemainRecoverable(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "temporary", http.StatusServiceUnavailable)
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
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   1,
	})

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.Error(t, err)
	assert.True(t, isTransientError(err), "exhausted transient errors must stay recoverable")

	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, 1, store.claims)
	assert.Zero(t, store.failed, "a transient dependency exhaustion must not mark the report failed")
	assert.Empty(t, store.completedKey)
	assert.Zero(t, storage.uploadCalls)
}

func TestProcessReportRetriesTransientS3Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMonstersJSON(w)
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
	storage := &workerObjectStorage{
		uploadErrs: []error{errors.New("network reset"), nil},
	}
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   2,
	})

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.NoError(t, err)

	assert.Equal(t, 2, storage.uploadCalls)
	assert.Equal(t, 1, store.claims)
	assert.Zero(t, store.failed)
	assert.NotEmpty(t, store.completedKey)
}

func TestProcessReportExhaustedS3RetriesRemainRecoverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMonstersJSON(w)
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
	storage := &workerObjectStorage{
		uploadErrs: []error{
			errors.New("network reset"),
			errors.New("network reset"),
		},
	}
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   1,
	})

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.Error(t, err)
	assert.True(t, isTransientError(err), "exhausted transient errors must stay recoverable")

	assert.Equal(t, 2, storage.uploadCalls)
	assert.Equal(t, 1, store.claims)
	assert.Zero(t, store.failed, "a transient dependency exhaustion must not mark the report failed")
	assert.Empty(t, store.completedKey)
}

func TestProcessReportDoesNotRetryPermanentS3Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMonstersJSON(w)
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
	storage := &workerObjectStorage{
		uploadErrs: []error{retry.Permanent(errors.New("invalid bucket"))},
	}
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   3,
	})

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.Error(t, err)

	assert.Equal(t, 1, storage.uploadCalls, "a permanent S3 failure must not be retried")
	assert.Equal(t, 1, store.claims)
	assert.Equal(t, 1, store.failed)
	assert.Empty(t, store.completedKey)
}

func TestProcessReportRenewsLeaseDuringRetryWait(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		writeMonstersJSON(w)
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
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: 60 * time.Millisecond,
		MaxDelay:     60 * time.Millisecond,
		MaxRetries:   1,
	})
	worker.leaseRenewInterval = 5 * time.Millisecond

	err := worker.ProcessReport(context.Background(), userID, reportID)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&store.leaseUpdates), int32(1),
		"the lease must be renewed while waiting between dependency attempts")
	assert.NotEmpty(t, store.completedKey)
}

func TestProcessReportCancellationDoesNotMarkFailed(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
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
	worker := newReportTestWorker(server, store, storage, retry.Policy{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		MaxRetries:   3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	err := worker.ProcessReport(ctx, userID, reportID)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, store.failed, "shutdown cancellation must not permanently fail the report")
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
