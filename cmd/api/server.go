package api

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

	"github.com/iamonah/drift/config"
	"github.com/rs/zerolog"
)

func (app *App) Serve() error {
	errorlog := zerolog.New(app.log).With().Str("component", "http").Logger()

	idleTimeout, readTimeout, writeTimeout, err := timeConv(app.cfg.Server)
	if err != nil {
		return fmt.Errorf("failed to convert time durations: %w", err)
	}

	srv := &http.Server{
		Addr:         net.JoinHostPort("127.0.0.1", app.cfg.Server.Port),
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

	app.log.Info().
		Str("port", app.cfg.Server.Port).
		Str("env", app.cfg.Environment).
		Msg("starting server")

	err = srv.ListenAndServe()
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
	idleTimeout, err = time.ParseDuration(dur.IdleTimeout)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse idle timeout: %w", err)
	}
	readTimeout, err = time.ParseDuration(dur.ReadTimeout)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse read timeout: %w", err)
	}
	writeTimeout, err = time.ParseDuration(dur.WriteTimeout)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse write timeout: %w", err)
	}

	return idleTimeout, readTimeout, writeTimeout, nil
}
