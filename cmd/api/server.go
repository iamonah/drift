package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/infra/database/driftdb"
	"github.com/iamonah/drift/internal/infra/s3"
	"github.com/iamonah/drift/internal/infra/sqs"
	"github.com/iamonah/drift/internal/util"
	"github.com/rs/zerolog"
)

type App struct {
	cfg           *config.Config
	Store         *driftdb.Store
	log           zerolog.Logger
	jwtMaker      util.TokenMaker
	awsSQS        sqs.Queue
	queueURL      string
	ObjectStorage s3.ObjectStorage
	router        *mux.Router
}

func NewApp(cfg *config.Config, mux *mux.Router, store *driftdb.Store, log zerolog.Logger, queue sqs.Queue, queueURL string, objectStorage s3.ObjectStorage) *App {
	jwtMaker := util.NewJWTMaker(cfg.JWT.SecretKey)

	return &App{
		cfg:           cfg,
		Store:         store,
		log:           log,
		jwtMaker:      &jwtMaker,
		awsSQS:        queue,
		queueURL:      queueURL,
		ObjectStorage: objectStorage,
		router:        mux,
	}
}

// logger returns the request-scoped logger set by the Logger middleware: the
// app logger with the request_id attached.
func (a *App) logger(r *http.Request) *zerolog.Logger {
	return zerolog.Ctx(r.Context())
}

func (app *App) Serve() error {
	errorlog := zerolog.New(app.log).With().Str("component", "http").Logger()

	idleTimeout, readTimeout, writeTimeout, err := timeConv(app.cfg.Server)
	if err != nil {
		return fmt.Errorf("failed to convert time durations: %w", err)
	}

	srv := &http.Server{
		Addr:         net.JoinHostPort(app.cfg.Server.Host, app.cfg.Server.Port),
		Handler:      app.mux(),
		IdleTimeout:  idleTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		ErrorLog:     log.New(errorlog, "", 0),
	}

	shutdownError := make(chan error, 1)

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		defer signal.Stop(quit)

		sig := <-quit
		app.log.Info().Str("signal", sig.String()).Msg("shutting down server")

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		shutdownError <- srv.Shutdown(ctx)
	}()

	publisherCtx, stopPublisher := context.WithCancel(context.Background())
	publisherDone := make(chan struct{})
	go func() {
		defer close(publisherDone)
		app.runOutboxPublisher(publisherCtx)
	}()
	defer func() {
		stopPublisher()

		timer := time.NewTimer(outboxShutdownGrace)
		defer timer.Stop()

		select {
		case <-publisherDone:
		case <-timer.C:
			app.log.Warn().
				Dur("grace", outboxShutdownGrace).
				Msg("outbox publisher did not stop before shutdown grace expired")
		}
	}()

	app.log.Info().
		Str("port", app.cfg.Server.Port).
		Str("env", app.cfg.Environment).
		Msg("starting server")

	err = srv.ListenAndServe()
	stopPublisher()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	if err := <-shutdownError; err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	app.log.Info().Msg("server stopped")

	return nil
}

func timeConv(dur config.Server) (idleTimeout, readTimeout, writeTimeout time.Duration, err error) {
	idleTimeout, err = config.ParseDuration(dur.IdleTimeout)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse idle timeout: %w", err)
	}
	readTimeout, err = config.ParseDuration(dur.ReadTimeout)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse read timeout: %w", err)
	}
	writeTimeout, err = config.ParseDuration(dur.WriteTimeout)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse write timeout: %w", err)
	}

	return idleTimeout, readTimeout, writeTimeout, nil
}
