package main

import (
	"context"
	"log"
	"time"

	"github.com/iamonah/drift/cmd/api"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/database"
	"github.com/iamonah/drift/internal/database/driftdb"
	"github.com/rs/zerolog"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log := zerolog.New(zerolog.ConsoleWriter{Out: log.Writer()}).With().Timestamp().Logger()
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
	db.Ping(ctx)

	err = database.MigrateDB(db)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to migrate database")
	}

	store := driftdb.NewStore(db.Client)

	api := api.NewApp(cfg, store, log)
	err = api.Serve()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to serve API")
	}
	log.Println("Database migration completed successfully.")

}
