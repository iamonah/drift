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
worker:
  concurrency: 6
  retry:
    initial_delay: 3s
    max_delay: 45s
    max_retries: 2
outbox:
  interval: 2s
  batch_size: 10
  concurrency: 2
  send_timeout: 3s
  claim_lease: 2m
  max_attempts: 4
  retry_base: 1s
  retry_max: 10s
  failure_threshold: 3
  cooldown: 20s
awssqs:
  queue_name: my-test-queue
  base_url: http://localhost:4566
awss3:
  bucket_name: my-test-bucket
  base_url: http://localhost:4566
  presign_expiration: 5m
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
	if cfg.AWSS3.PresignExpiration != "5m" {
		t.Fatalf("LoadConfigFile() presign expiration = %q, want 5m", cfg.AWSS3.PresignExpiration)
	}
	if cfg.Worker.Concurrency != 6 {
		t.Fatalf("LoadConfigFile() worker concurrency = %d, want 6", cfg.Worker.Concurrency)
	}
	if cfg.Worker.Retry.InitialDelay != 3*time.Second ||
		cfg.Worker.Retry.MaxDelay != 45*time.Second ||
		cfg.Worker.Retry.MaxRetries != 2 {
		t.Fatalf("LoadConfigFile() worker retry = %#v, want decoded durations", cfg.Worker.Retry)
	}
	if cfg.Outbox.Interval != 2*time.Second ||
		cfg.Outbox.BatchSize != 10 ||
		cfg.Outbox.Concurrency != 2 ||
		cfg.Outbox.SendTimeout != 3*time.Second ||
		cfg.Outbox.ClaimLease != 2*time.Minute ||
		cfg.Outbox.MaxAttempts != 4 ||
		cfg.Outbox.RetryBase != time.Second ||
		cfg.Outbox.RetryMax != 10*time.Second ||
		cfg.Outbox.FailureThreshold != 3 ||
		cfg.Outbox.Cooldown != 20*time.Second {
		t.Fatalf("LoadConfigFile() outbox = %#v, want decoded durations", cfg.Outbox)
	}
}

func TestApplyWorkerRetryDefaults(t *testing.T) {
	var retry WorkerRetry
	applyWorkerRetryDefaults(&retry)

	if retry.InitialDelay != defaultWorkerRetryInitial ||
		retry.MaxDelay != defaultWorkerRetryMax ||
		retry.MaxRetries != defaultWorkerRetryAttempts {
		t.Fatalf("applyWorkerRetryDefaults() = %#v, want defaults", retry)
	}
	if err := validateWorkerRetry(retry); err != nil {
		t.Fatalf("validateWorkerRetry(defaults) error = %v", err)
	}
}

func TestValidateWorkerRetryRejectsMaxBelowInitial(t *testing.T) {
	retry := WorkerRetry{
		InitialDelay: 10 * time.Second,
		MaxDelay:     time.Second,
		MaxRetries:   3,
	}

	if err := validateWorkerRetry(retry); err == nil {
		t.Fatal("validateWorkerRetry() = nil, want max-delay error")
	}
}

func TestApplyOutboxDefaults(t *testing.T) {
	var outbox Outbox
	applyOutboxDefaults(&outbox)

	if outbox.Interval != defaultOutboxInterval ||
		outbox.BatchSize != defaultOutboxBatchSize ||
		outbox.Concurrency != defaultOutboxConcurrency ||
		outbox.SendTimeout != defaultOutboxSendTimeout ||
		outbox.ClaimLease != defaultOutboxClaimLease ||
		outbox.MaxAttempts != defaultOutboxMaxAttempts ||
		outbox.RetryBase != defaultOutboxRetryBase ||
		outbox.RetryMax != defaultOutboxRetryMax ||
		outbox.FailureThreshold != defaultOutboxFailureThreshold ||
		outbox.Cooldown != defaultOutboxCooldown {
		t.Fatalf("applyOutboxDefaults() = %#v, want defaults", outbox)
	}
	if err := validateOutbox(outbox); err != nil {
		t.Fatalf("validateOutbox(defaults) error = %v", err)
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

func TestValidateOutboxRejectsShortLease(t *testing.T) {
	outbox := Outbox{
		Interval:    5 * time.Second,
		BatchSize:   20,
		Concurrency: 4,
		SendTimeout: 10 * time.Second,
		ClaimLease:  30 * time.Second,
		RetryBase:   time.Second,
		RetryMax:    10 * time.Second,
		Cooldown:    time.Second,
	}

	if err := validateOutbox(outbox); err == nil {
		t.Fatal("validateOutbox() = nil, want short-lease error")
	}
}

func TestValidateOutboxRejectsRetryMaxBelowBase(t *testing.T) {
	outbox := Outbox{
		Interval:    5 * time.Second,
		BatchSize:   1,
		Concurrency: 1,
		SendTimeout: 10 * time.Second,
		ClaimLease:  time.Minute,
		RetryBase:   10 * time.Second,
		RetryMax:    time.Second,
		Cooldown:    time.Second,
	}

	if err := validateOutbox(outbox); err == nil {
		t.Fatal("validateOutbox() = nil, want retry-max error")
	}
}
