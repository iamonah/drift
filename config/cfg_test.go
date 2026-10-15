package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.yaml")
	contents := []byte(`environment: test
database:
  host: localhost
  port: "5433"
  user: drifttest
  password: secret
  name: drift_test
  ssl_mode: disable
  conn_max_lifetime: 30m
  conn_max_idle_time: 5m
  max_conns: 25
  min_conns: 5
server:
  port: "8080"
  read_timeout: 10s
  write_timeout: 30s
  idle_timeout: 60s
  host: 127.0.0.1
  cors_allowed_origins:
    - http://localhost:3000
awssqs:
  queue_name: my-test-queue
  base_url: http://localhost:4566
awss3:
  bucket_name: my-test-bucket
  base_url: http://localhost:4566
jwt:
  secret_key: test-jwt-secret
  issuer: drift
  audience: drift
  access_token_duration: 15m
  refresh_token_duration: 7d
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.Environment != "test" || cfg.DB.Port != "5433" || cfg.DB.Name != "drift_test" {
		t.Fatalf("LoadConfigFile() = %#v, want decoded test configuration", cfg)
	}
	if cfg.Server.ReadTimeout != "10s" || cfg.DB.MaxConns != 25 {
		t.Fatalf("LoadConfigFile() = %#v, want decoded durations and connection limits", cfg)
	}
}

func TestParseDuration(t *testing.T) {
	duration, err := ParseDuration("7d")
	if err != nil {
		t.Fatalf("ParseDuration() error = %v", err)
	}
	if duration != 7*24*time.Hour {
		t.Fatalf("ParseDuration() = %v, want %v", duration, 7*24*time.Hour)
	}
}
