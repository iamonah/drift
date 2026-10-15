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

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	db, err := database.NewDB(&cfg.DB)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = db.Ping(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database")
	}

	err = database.MigrateDB(db)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to migrate database")
	}
	log.Info().Msg("database migration completed successfully")

	store := driftdb.NewStore(db.Client)

	sdkConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load AWS config")
	}

	s3Actions := objectstorage.NewS3Actions(sdkConfig, cfg.AWSS3.BaseURL)
	sqs := queue.NewSQSActions(sdkConfig, cfg.AWSSQS.BaseURL)

	api := NewApp(cfg, store, log, s3Actions, sqs)
	err = api.Serve()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to serve API")
	}
}
