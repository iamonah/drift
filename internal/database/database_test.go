package database

import (
	"context"
	"testing"
	"time"

	"github.com/iamonah/drift/config"
)

var testCfg = &config.Database{
	Host:            "localhost",
	Port:            "5433",
	User:            "drifttest",
	Password:        "secret",
	Name:            "drift_test",
	SSLMode:         "disable",
	ConnMaxLifetime: "30s",
	ConnMaxIdleTime: "5s",
	MaxConns:        25,
	MinConns:        5,
}

func TestDatabaseURL(t *testing.T) {
	got := DatabaseURL(testCfg)
	want := "postgres://drifttest:secret@localhost:5433/drift_test?sslmode=disable"
	if got != want {
		t.Fatalf("databaseURL() = %q, want %q", got, want)
	}
}

func TestNewDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbClient, err := NewDB(testCfg)
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	defer dbClient.Client.Close()

	if dbClient.Client == nil {
		t.Fatal("NewDB() returned nil Client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = dbClient.Ping(ctx)
	if err != nil {
		t.Fatalf("dbClient.Ping() error = %v", err)
	}
}
