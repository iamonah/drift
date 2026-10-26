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

const defaultWorkerConcurrency = 4

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

type Config struct {
	Environment string   `koanf:"environment" validate:"required"`
	Server      Server   `koanf:"server" validate:"required"`
	DB          Database `koanf:"database" validate:"required"`
	AWSSQS      AWSSQS   `koanf:"awssqs" validate:"required"`
	AWSS3       AWSS3    `koanf:"awss3" validate:"required"`
	JWT         JWT      `koanf:"jwt" validate:"required"`
	Worker      Worker   `koanf:"worker"`
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

	return &config, nil
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
