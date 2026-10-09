package api

import (
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/database/driftdb"
	"github.com/iamonah/drift/internal/util"
	"github.com/rs/zerolog"
)

type App struct {
	cfg      *config.Config
	Store    *driftdb.Store
	log      zerolog.Logger
	jwtMaker util.TokenMaker
}

func NewApp(cfg *config.Config, store *driftdb.Store, log zerolog.Logger) *App {

	return &App{
		cfg:   cfg,
		Store: store,
		log:   log,
	}
}
