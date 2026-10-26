package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"uuid"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/client/loz"
	"github.com/iamonah/drift/internal/infra/database"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/s3"
	"github.com/iamonah/drift/internal/infra/sqs"
	"github.com/iamonah/drift/internal/job"
	"github.com/rs/zerolog"
)

type Worker struct {
	cfg           *config.Config
	ObjectStorage s3.ObjectStorage
	Queue         sqs.Queue
	reportStore   *driftdb.Store
	loz           *loz.Client
	log           zerolog.Logger
	process       func(ctx context.Context, userID uuid.UUID, reportID uuid.UUID) error
	concurrency   int
}

func NewWorker(cfg *config.Config, objectStorage s3.ObjectStorage, queue sqs.Queue, reportStore *driftdb.Store, lozClient *loz.Client, log zerolog.Logger) *Worker {
	concurrency := 1
	if cfg != nil && cfg.Worker.Concurrency > 0 {
		concurrency = cfg.Worker.Concurrency
	}

	w := &Worker{
		cfg:           cfg,
		ObjectStorage: objectStorage,
		Queue:         queue,
		reportStore:   reportStore,
		loz:           lozClient,
		log:           log,
		concurrency:   concurrency,
	}
	w.process = w.ProcessReport

	return w
}

// Start polls SQS and dispatches messages to a fixed pool of workers.
// It returns only after all worker goroutines have exited.
func (w *Worker) Start(ctx context.Context) error {
	queueURL, err := w.Queue.GetQueueURL(ctx, w.cfg.AWSSQS.QueueName)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("get queue URL: %w", err)
	}

	concurrency := max(1, w.concurrency)
	batchSize := min(int32(concurrency), int32(10))
	jobs := make(chan sqs.Message, concurrency)

	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for message := range jobs {
				w.handleMessage(ctx, queueURL, message)
			}
		}()
	}

	w.log.Info().
		Str("queue_url", queueURL).
		Int("concurrency", concurrency).
		Msg("worker started")

	defer func() {
		close(jobs)
		workers.Wait()
		w.log.Info().Msg("worker stopped")
	}()

	for {
		messages, err := w.Queue.ReceiveMessages(ctx, queueURL, batchSize, 10)
		if err != nil {
			if ctx.Err() != nil { //error is due to context cancellation, exit gracefully
				return nil
			}

			w.log.Error().Err(err).Msg("receive messages")

			select {
			case <-time.After(time.Second): //could become exponential backoff if needed
			case <-ctx.Done():
				return nil
			}
			continue //keep polling for messages even after an error
		}

		for _, message := range messages {
			select {
			case jobs <- message:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (w *Worker) handleMessage(ctx context.Context, queueURL string, message sqs.Message) {
	var payload job.Report
	if err := json.Unmarshal([]byte(message.Body), &payload); err != nil {
		w.log.Error().Err(err).Str("message_id", message.ID).Msg("discarding malformed message")
		w.deleteMessage(ctx, queueURL, message, "")
		return
	}

	reportID := payload.ID.String()

	err := w.process(ctx, payload.UserID, payload.ID)
	switch {
	case err == nil:
		// The report completed or is already in a terminal state.
		w.deleteMessage(ctx, queueURL, message, reportID)
	case errors.Is(err, driftdb.ErrReportNotFound):
		w.log.Warn().Str("report_id", reportID).Msg("report no longer exists; discarding message")
		w.deleteMessage(ctx, queueURL, message, reportID)
	case errors.Is(err, driftdb.ErrReportProcessing), errors.Is(err, driftdb.ErrReportOwnershipLost):
		w.log.Info().Str("report_id", reportID).Msg("report is held by another worker; deferring message")
		w.deferMessage(ctx, queueURL, message, reportID)
	default:
		// Leave the message for SQS redelivery.
		w.log.Error().Err(err).Str("report_id", reportID).Msg("report processing failed; leaving message for redelivery")
	}
}

func (w *Worker) deleteMessage(ctx context.Context, queueURL string, message sqs.Message, reportID string) {
	if err := w.Queue.DeleteMessage(ctx, queueURL, message.ReceiptHandle); err != nil {
		w.log.Error().Err(err).
			Str("message_id", message.ID).
			Str("report_id", reportID).
			Msg("delete message")
	}
}

func (w *Worker) deferMessage(ctx context.Context, queueURL string, message sqs.Message, reportID string) {
	timeout := int32(reportDeferDuration / time.Second)

	if err := w.Queue.ChangeMessageVisibility(ctx, queueURL, message.ReceiptHandle, timeout); err != nil {
		w.log.Error().Err(err).
			Str("message_id", message.ID).
			Str("report_id", reportID).
			Dur("defer", reportDeferDuration).
			Msg("defer message")
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	db, err := database.NewDB(&cfg.DB)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create database")
	}
	defer db.Client.Close()

	log.Info().Msg("database connection established successfully")
	store := driftdb.NewStore(db.Client)

	sdkConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load AWS config")
	}

	log.Info().Msg("AWS config loaded successfully")

	s3Actions := s3.NewS3Actions(sdkConfig, cfg.AWSS3.BaseURL)
	queue := sqs.NewSQSActions(sdkConfig, cfg.AWSSQS.BaseURL)
	lozClient := loz.NewClient(loz.BaseUrl, &http.Client{Timeout: 30 * time.Second})

	worker := NewWorker(cfg, s3Actions, queue, store, lozClient, log)

	if err := worker.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("worker stopped with error")
	}
}
