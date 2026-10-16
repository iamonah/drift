package main

import (
	"context"
	"os"
	"time"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/iamonah/drift/config"

	"github.com/iamonah/drift/internal/infra/database"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	objectstorage "github.com/iamonah/drift/internal/infra/s3"
	queue "github.com/iamonah/drift/internal/infra/sqs"

	"github.com/rs/zerolog"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	// Fall back to this logger when a context carries no request-scoped logger,
	// so zerolog.Ctx never hands back a logger that silently discards output.
	zerolog.DefaultContextLogger = &log

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.NewDB(&cfg.DB)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create database")
	}
	defer db.Client.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database")
	}

	if err := database.MigrateDB(db); err != nil {
		log.Fatal().Err(err).Msg("Failed to migrate database")
	}
	log.Info().Msg("database migration completed successfully")

	store := driftdb.NewStore(db.Client)

	sdkConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load AWS config")
	}

	s3Actions := objectstorage.NewS3Actions(sdkConfig, cfg.AWSS3.BaseURL)
	queueActions := queue.NewSQSActions(sdkConfig, cfg.AWSSQS.BaseURL)

	queueURL, err := queueActions.GetQueueURL(ctx, cfg.AWSSQS.QueueName)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to resolve the reports queue URL")
	}

	api := NewApp(cfg, store, log, s3Actions, queueActions, queueURL)
	if err := api.Serve(); err != nil {
		log.Fatal().Err(err).Msg("Failed to serve API")
	}
}
