package database

import (
	"testing"

	"github.com/iamonah/drift/config"
)

func TestDatabaseURL(t *testing.T) {
	cfg := &config.Database{
		Host:            "localhost",
		Port:            "5432",
		User:            "driftadmin",
		Password:        "p@ss word",
		Name:            "drift",
		SSLMode:         "disable",
		ConnMaxLifetime: "30s",
		ConnMaxIdleTime: "5s",
		MaxConns:        25,
		MinConns:        5,
	}

	got := databaseURL(cfg)
	want := "postgres://driftadmin:p%40ss%20word@localhost:5432/drift?sslmode=disable"
	if got != want {
		t.Fatalf("databaseURL() = %q, want %q", got, want)
	}
}
