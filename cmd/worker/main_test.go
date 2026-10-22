package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"uuid"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/sqs"
	"github.com/iamonah/drift/internal/job"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeQueue struct {
	deleted   []string
	deferred  []int32
	deleteErr error
	deferErr  error
	onDelete  func()
}

func (q *fakeQueue) GetQueueURL(ctx context.Context, queueName string) (string, error) {
	return "queue-url", nil
}

func (q *fakeQueue) SendMessage(ctx context.Context, queueURL string, body any, options *sqs.SendOptions) (string, error) {
	return "", nil
}

func (q *fakeQueue) ReceiveMessages(ctx context.Context, queueURL string, maxMessages, waitSeconds int32) ([]sqs.Message, error) {
	return nil, nil
}

func (q *fakeQueue) DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error {
	if q.onDelete != nil {
		q.onDelete()
	}
	q.deleted = append(q.deleted, receiptHandle)

	return q.deleteErr
}

func (q *fakeQueue) ChangeMessageVisibility(ctx context.Context, queueURL, receiptHandle string, timeoutSeconds int32) error {
	q.deferred = append(q.deferred, timeoutSeconds)

	return q.deferErr
}

func newTestWorker(queue sqs.Queue, process func(context.Context, uuid.UUID, uuid.UUID) error) (*Worker, *bytes.Buffer) {
	var logs bytes.Buffer
	worker := NewWorker(&config.Config{}, nil, queue, nil, nil, zerolog.New(&logs))
	worker.process = process

	return worker, &logs
}

func testMessage(t *testing.T, reportID uuid.UUID) sqs.Message {
	t.Helper()

	body, err := json.Marshal(job.Report{ID: reportID, UserID: uuid.New(), Type: "monsters"})
	require.NoError(t, err)

	return sqs.Message{ID: "message-1", Body: string(body), ReceiptHandle: "receipt-1"}
}

func TestHandleMessage_DeletesAfterCompletion(t *testing.T) {
	var order []string

	queue := &fakeQueue{onDelete: func() { order = append(order, "delete") }}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		order = append(order, "completed")
		return nil
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, uuid.New()))

	assert.Equal(t, []string{"completed", "delete"}, order, "the message must be acknowledged after completion")
	assert.Equal(t, []string{"receipt-1"}, queue.deleted)
	assert.Empty(t, queue.deferred)
}

func TestHandleMessage_TerminalReportIsDeleted(t *testing.T) {
	queue := &fakeQueue{}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		// ProcessReport returns nil when ClaimReport finds the report terminal.
		return nil
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, uuid.New()))

	assert.Equal(t, []string{"receipt-1"}, queue.deleted)
	assert.Empty(t, queue.deferred)
}

func TestHandleMessage_MissingReportIsDeleted(t *testing.T) {
	queue := &fakeQueue{}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		return fmt.Errorf("claimreport: %w", driftdb.ErrReportNotFound)
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, uuid.New()))

	assert.Equal(t, []string{"receipt-1"}, queue.deleted)
	assert.Empty(t, queue.deferred)
}

func TestHandleMessage_ProcessingElsewhereIsDeferred(t *testing.T) {
	queue := &fakeQueue{}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		return fmt.Errorf("claimreport: %w", driftdb.ErrReportProcessing)
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, uuid.New()))

	assert.Empty(t, queue.deleted, "a report owned by another worker must not be acknowledged")
	assert.Equal(t, []int32{60}, queue.deferred)
}

func TestHandleMessage_OwnershipLostIsDeferred(t *testing.T) {
	queue := &fakeQueue{}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		return fmt.Errorf("markcompleted: %w", driftdb.ErrReportOwnershipLost)
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, uuid.New()))

	assert.Empty(t, queue.deleted, "a worker that lost the claim must not acknowledge the message")
	assert.Equal(t, []int32{60}, queue.deferred)
}

func TestHandleMessage_ProcessingErrorIsNotAcknowledged(t *testing.T) {
	queue := &fakeQueue{}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		return errors.New("processing failed")
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, uuid.New()))

	assert.Empty(t, queue.deleted)
	assert.Empty(t, queue.deferred)
}

func TestHandleMessage_MalformedMessageIsDeleted(t *testing.T) {
	queue := &fakeQueue{}
	worker, _ := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		t.Fatal("process must not run for a malformed message")
		return nil
	})

	message := sqs.Message{ID: "message-1", Body: "not json", ReceiptHandle: "receipt-1"}
	worker.handleMessage(context.Background(), "queue-url", message)

	assert.Equal(t, []string{"receipt-1"}, queue.deleted)
	assert.Empty(t, queue.deferred)
}

func TestHandleMessage_DeleteFailureIsLogged(t *testing.T) {
	queue := &fakeQueue{deleteErr: errors.New("sqs unavailable")}
	reportID := uuid.New()
	worker, logs := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		return nil
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, reportID))

	assert.Contains(t, logs.String(), "delete message")
	assert.Contains(t, logs.String(), reportID.String())
	assert.Empty(t, queue.deferred)
}

func TestHandleMessage_DeferFailureIsLogged(t *testing.T) {
	queue := &fakeQueue{deferErr: errors.New("sqs unavailable")}
	reportID := uuid.New()
	worker, logs := newTestWorker(queue, func(context.Context, uuid.UUID, uuid.UUID) error {
		return fmt.Errorf("claimreport: %w", driftdb.ErrReportProcessing)
	})

	worker.handleMessage(context.Background(), "queue-url", testMessage(t, reportID))

	assert.Contains(t, logs.String(), "defer message")
	assert.Contains(t, logs.String(), reportID.String())
	assert.Empty(t, queue.deleted)
}
