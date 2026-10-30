package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	defaultWorkerConcurrency      = 4
	defaultOutboxInterval         = 5 * time.Second
	defaultOutboxBatchSize        = 20
	defaultOutboxConcurrency      = 4
	defaultOutboxSendTimeout      = 10 * time.Second
	defaultOutboxClaimLease       = 2 * time.Minute
	defaultOutboxMaxAttempts      = 10
	defaultOutboxRetryBase        = 5 * time.Second
	defaultOutboxRetryMax         = 5 * time.Minute
	defaultOutboxFailureThreshold = 5
	defaultOutboxCooldown         = 30 * time.Second
	defaultOutboxLeasePadding     = 5 * time.Second
)

// OutboxCleanupTimeout is how long the publisher allows for the final database
// transition after a send. It is also part of the minimum claim-lease check.
const OutboxCleanupTimeout = 5 * time.Second

type Database struct {
	Host            string `koanf:"host" validate:"required"`
	Port            string `koanf:"port" validate:"required"`
	User            string `koanf:"user" validate:"required"`
	Password        string `koanf:"password" validate:"required"`
	Name            string `koanf:"name" validate:"required"`
	SSLMode         string `koanf:"ssl_mode"`
	ConnMaxLifetime string `koanf:"conn_max_lifetime"`
	ConnMaxIdleTime string `koanf:"conn_max_idle_time"`
	MaxConns        int    `koanf:"max_conns"`
	MinConns        int    `koanf:"min_conns"`
}

type Server struct {
	Port               string   `koanf:"port" validate:"required"`
	ReadTimeout        string   `koanf:"read_timeout"`
	WriteTimeout       string   `koanf:"write_timeout"`
	IdleTimeout        string   `koanf:"idle_timeout"`
	Host               string   `koanf:"host" validate:"required"`
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins" validate:"required"`
}

type Worker struct {
	Concurrency int `koanf:"concurrency" validate:"min=1"`
}

// Outbox controls the API-side report outbox publisher. MaxAttempts applies
// only to permanent, per-message failures. Transient and global failures (for
// example an SQS outage or an expired credential) are retried without consuming
// that budget so a broken dependency cannot dead-letter the whole backlog.
// FailureThreshold is the number of consecutive cycles where every send fails
// before the publisher pauses for Cooldown.
type Outbox struct {
	Interval         time.Duration `koanf:"interval"` // How often to check the outbox for pending reports
	BatchSize        int           `koanf:"batch_size" validate:"min=1,max=100"`
	Concurrency      int           `koanf:"concurrency" validate:"min=1,max=50"`
	SendTimeout      time.Duration `koanf:"send_timeout"`
	ClaimLease       time.Duration `koanf:"claim_lease"`
	MaxAttempts      int           `koanf:"max_attempts" validate:"min=1,max=100"`
	RetryBase        time.Duration `koanf:"retry_base"`
	RetryMax         time.Duration `koanf:"retry_max"`
	FailureThreshold int           `koanf:"failure_threshold" validate:"min=1,max=100"`
	Cooldown         time.Duration `koanf:"cooldown"`
}

type Config struct {
	Environment string   `koanf:"environment" validate:"required"`
	Server      Server   `koanf:"server" validate:"required"`
	DB          Database `koanf:"database" validate:"required"`
	AWSSQS      AWSSQS   `koanf:"awssqs" validate:"required"`
	AWSS3       AWSS3    `koanf:"awss3" validate:"required"`
	JWT         JWT      `koanf:"jwt" validate:"required"`
	Worker      Worker   `koanf:"worker"`
	Outbox      Outbox   `koanf:"outbox"`
}

type AWSSQS struct {
	QueueName string `koanf:"queue_name" validate:"required"`
	BaseURL   string `koanf:"base_url" validate:"required"` //localstack endpoint URL for SQS
}

type AWSS3 struct {
	BucketName        string `koanf:"bucket_name" validate:"required"`
	BaseURL           string `koanf:"base_url" validate:"required"` //localstack endpoint URL for S3
	PresignExpiration string `koanf:"presign_expiration"`           //link expiration time
}

type JWT struct {
	SecretKey            string `koanf:"secret_key" validate:"required"`
	Issuer               string `koanf:"issuer" validate:"required"`
	Audience             string `koanf:"audience" validate:"required"`
	AccessTokenDuration  string `koanf:"access_token_duration" validate:"required"`
	RefreshTokenDuration string `koanf:"refresh_token_duration" validate:"required"`
}

func LoadConfig() (*Config, error) {
	return LoadConfigFile("app.yaml")
}

func LoadConfigFile(path string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {

		return nil, fmt.Errorf("loadconfig: load %s: %w", path, err)
	}

	// Environment variables override YAML values.
	if err := k.Load(env.Provider(".", env.Opt{
		TransformFunc: func(key, value string) (string, any) {
			key = strings.ReplaceAll(key, "__", ".")
			return strings.ToLower(key), value
		},
	}), nil); err != nil {
		return nil, fmt.Errorf("loadconfig: load environment: %w", err)
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("loadconfig: decode: %w", err)
	}

	if config.AWSS3.PresignExpiration == "" {
		config.AWSS3.PresignExpiration = "2m"
	}
	if config.Worker.Concurrency == 0 {
		config.Worker.Concurrency = defaultWorkerConcurrency
	}
	applyOutboxDefaults(&config.Outbox)

	validate := validator.New()
	if err := validate.Struct(config); err != nil {
		return nil, fmt.Errorf("loadconfig: validate: %w", err)
	}

	if _, err := ParseDuration(config.JWT.AccessTokenDuration); err != nil {
		return nil, fmt.Errorf("loadconfig: invalid access token duration: %w", err)
	}
	if _, err := ParseDuration(config.JWT.RefreshTokenDuration); err != nil {
		return nil, fmt.Errorf("loadconfig: invalid refresh token duration: %w", err)
	}
	if _, err := ParseDuration(config.Server.ReadTimeout); err != nil {
		return nil, fmt.Errorf("loadconfig: invalid read timeout: %w", err)
	}
	if _, err := ParseDuration(config.Server.WriteTimeout); err != nil {
		return nil, fmt.Errorf("loadconfig: invalid write timeout: %w", err)
	}
	if _, err := ParseDuration(config.Server.IdleTimeout); err != nil {
		return nil, fmt.Errorf("loadconfig: invalid idle timeout: %w", err)
	}
	presignExpiration, err := ParseDuration(config.AWSS3.PresignExpiration)
	if err != nil {
		return nil, fmt.Errorf("loadconfig: invalid S3 presign expiration: %w", err)
	}
	if presignExpiration <= 0 {
		return nil, fmt.Errorf("loadconfig: S3 presign expiration must be positive")
	}

	if err := validateOutbox(config.Outbox); err != nil {
		return nil, fmt.Errorf("loadconfig: %w", err)
	}

	return &config, nil
}

func applyOutboxDefaults(outbox *Outbox) {
	if outbox.Interval == 0 {
		outbox.Interval = defaultOutboxInterval
	}
	if outbox.BatchSize == 0 {
		outbox.BatchSize = defaultOutboxBatchSize
	}
	if outbox.Concurrency == 0 {
		outbox.Concurrency = defaultOutboxConcurrency
	}
	if outbox.SendTimeout == 0 {
		outbox.SendTimeout = defaultOutboxSendTimeout
	}
	if outbox.ClaimLease == 0 {
		outbox.ClaimLease = defaultOutboxClaimLease
	}
	if outbox.MaxAttempts == 0 {
		outbox.MaxAttempts = defaultOutboxMaxAttempts
	}
	if outbox.RetryBase == 0 {
		outbox.RetryBase = defaultOutboxRetryBase
	}
	if outbox.RetryMax == 0 {
		outbox.RetryMax = defaultOutboxRetryMax
	}
	if outbox.FailureThreshold == 0 {
		outbox.FailureThreshold = defaultOutboxFailureThreshold
	}
	if outbox.Cooldown == 0 {
		outbox.Cooldown = defaultOutboxCooldown
	}
}

func validateOutbox(outbox Outbox) error {
	if outbox.Interval <= 0 {
		return fmt.Errorf("outbox interval must be positive")
	}
	if outbox.SendTimeout <= 0 {
		return fmt.Errorf("outbox send timeout must be positive")
	}
	if outbox.ClaimLease <= 0 {
		return fmt.Errorf("outbox claim lease must be positive")
	}
	if outbox.RetryBase <= 0 {
		return fmt.Errorf("outbox retry base must be positive")
	}
	if outbox.RetryMax <= 0 {
		return fmt.Errorf("outbox retry max must be positive")
	}
	if outbox.RetryMax < outbox.RetryBase {
		return fmt.Errorf("outbox retry max must be greater than or equal to retry base")
	}
	if outbox.Cooldown <= 0 {
		return fmt.Errorf("outbox cooldown must be positive")
	}

	// A claim must remain valid for the worst-case time the publisher may spend
	// sending and finalizing a full batch. Otherwise a live publisher could lose
	// a claim while it is still sending, allowing another instance to send the
	// same entry.
	if outbox.BatchSize > 0 && outbox.Concurrency > 0 {
		waves := (outbox.BatchSize + outbox.Concurrency - 1) / outbox.Concurrency
		perEntry := outbox.SendTimeout + OutboxCleanupTimeout
		minLease := time.Duration(waves)*perEntry + defaultOutboxLeasePadding
		if outbox.ClaimLease < minLease {
			return fmt.Errorf(
				"outbox claim lease %s is too short for a %d-entry batch at concurrency %d with a %s send timeout (minimum %s)",
				outbox.ClaimLease,
				outbox.BatchSize,
				outbox.Concurrency,
				outbox.SendTimeout,
				minLease,
			)
		}
	}

	return nil
}

func ParseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration, nil
	}

	if !strings.HasSuffix(value, "d") {
		return 0, err
	}

	days, parseErr := strconv.ParseFloat(strings.TrimSuffix(value, "d"), 64)
	if parseErr != nil {
		return 0, err
	}

	return time.Duration(days * 24 * float64(time.Hour)), nil
}
