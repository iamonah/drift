package config

import (
	"os"
	"path/filepath"
	"testing"
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
