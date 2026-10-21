package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	reportStore   *driftdb.Store //we requre report store
	loz           *loz.Client
	log           zerolog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

func NewWorker(
	cfg *config.Config,
	objectStorage s3.ObjectStorage,
	queue sqs.Queue,
	reportStore *driftdb.Store,
	loz *loz.Client,
	log zerolog.Logger,
) *Worker {
	return &Worker{
		cfg:           cfg,
		ObjectStorage: objectStorage,
		Queue:         queue,
		reportStore:   reportStore,
		loz:           loz,
		log:           log,
		done:          make(chan struct{}),
	}
}

// Start polls the reports queue until Stop is called.
func (w *Worker) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	defer close(w.done)

	queueURL, err := w.Queue.GetQueueURL(ctx, w.cfg.AWSSQS.QueueName)
	if err != nil {
		return fmt.Errorf("get queue url: %w", err)
	}

	w.log.Info().Str("queue_url", queueURL).Msg("worker started")

	for {
		messages, err := w.Queue.ReceiveMessages(ctx, queueURL, 1, 10)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			w.log.Error().Err(err).Msg("receive messages")
			time.Sleep(time.Second)
			continue
		}

		for _, message := range messages {
			w.handleMessage(ctx, queueURL, message)
		}
	}
}

// Stop cancels the receive loop and waits for the in-flight report to finish.
func (w *Worker) Stop() error {
	if w.cancel == nil {
		return nil
	}

	w.cancel()
	<-w.done

	return nil
}

func (w *Worker) handleMessage(ctx context.Context, queueURL string, message sqs.Message) {
	var payload job.Report
	if err := json.Unmarshal([]byte(message.Body), &payload); err != nil {
		w.log.Error().Err(err).Str("message_id", message.ID).Msg("discarding malformed message")
		w.deleteMessage(ctx, queueURL, message)
		return
	}

	if err := w.ProcessReport(ctx, payload.UserID, payload.ID); err != nil {
		if errors.Is(err, driftdb.ErrReportProcessing) {
			w.log.Info().Str("report_id", payload.ID.String()).Msg("report is already processing; leaving message for redelivery")
			return
		}

		// The report has already been marked failed, so a redelivery will claim
		// nothing and delete the message. Leaving it here avoids losing a report
		// whose failure could not be recorded.
		w.log.Error().Err(err).Str("report_id", payload.ID.String()).Msg("report processing failed; leaving message for redelivery")
		return
	}

	w.deleteMessage(ctx, queueURL, message)
}

func (w *Worker) deleteMessage(ctx context.Context, queueURL string, message sqs.Message) {
	if err := w.Queue.DeleteMessage(ctx, queueURL, message.ReceiptHandle); err != nil {
		w.log.Error().Err(err).Str("message_id", message.ID).Msg("delete message")
	}
}

func main() {
	ctx := context.Background()
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	database, err := database.NewDB(&cfg.DB)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create database")
	}
	log.Info().Msg("database connection established successfully")
	store := driftdb.NewStore(database.Client)

	sdkConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load AWS config")
	}
	log.Info().Msg("AWS config loaded successfully")

	s3Actions := s3.NewS3Actions(sdkConfig, cfg.AWSS3.BaseURL)
	queue := sqs.NewSQSActions(sdkConfig, cfg.AWSSQS.BaseURL)
	loz := loz.NewClient(loz.BaseUrl, &http.Client{Timeout: 30 * time.Second})

	worker := NewWorker(cfg, s3Actions, queue, store, loz, log)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		log.Info().Msg("stopping worker")
		if err := worker.Stop(); err != nil {
			log.Error().Err(err).Msg("stop worker")
		}
	}()

	if err := worker.Start(); err != nil {
		log.Fatal().Err(err).Msg("worker stopped with error")
	}
	log.Info().Msg("worker stopped")
}
