package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/iamonah/drift/config"
)

type DBClient struct {
	Client *sql.DB
}

func NewDB(cfg *config.Database) (*DBClient, error) {
	db, err := sql.Open("postgres", databaseURL(cfg))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxConns)
	db.SetMaxIdleConns(cfg.MinConns)

	connMaxLifetime, err := time.ParseDuration(cfg.ConnMaxLifetime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse conn_max_lifetime: %w", err)
	}
	db.SetConnMaxLifetime(connMaxLifetime)

	connMaxIdleTime, err := time.ParseDuration(cfg.ConnMaxIdleTime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse conn_max_idle_time: %w", err)
	}
	db.SetConnMaxIdleTime(connMaxIdleTime)

	return &DBClient{Client: db}, nil
}

func databaseURL(cfg *config.Database) string {
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   cfg.Name,
	}
	query := dsn.Query()
	query.Set("sslmode", cfg.SSLMode)
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func (dc *DBClient) Ping(ctx context.Context) error {
	if err := dc.Client.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	return nil
}
