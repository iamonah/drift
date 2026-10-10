package main

import (
	"context"
	"os"
	"time"

	"github.com/iamonah/drift/cmd/api"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/database"
	"github.com/iamonah/drift/internal/database/driftdb"
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

	api := api.NewApp(cfg, store, log)
	err = api.Serve()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to serve API")
	}
}
