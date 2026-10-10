package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"go.yaml.in/yaml/v4"
)

type Database struct {
	Host            string `yaml:"host" validate:"required"`
	Port            string `yaml:"port" validate:"required"`
	User            string `yaml:"user" validate:"required"`
	Password        string `yaml:"password" validate:"required"`
	Name            string `yaml:"name" validate:"required"`
	SSLMode         string `yaml:"ssl_mode"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime"`
	ConnMaxIdleTime string `yaml:"conn_max_idle_time"`
	MaxConns        int    `yaml:"max_conns"`
	MinConns        int    `yaml:"min_conns"`
}

type Server struct {
	Port               string   `yaml:"port" validate:"required"`
	ReadTimeout        string   `yaml:"read_timeout"`
	WriteTimeout       string   `yaml:"write_timeout"`
	IdleTimeout        string   `yaml:"idle_timeout"`
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins" validate:"required"`
}

type Config struct {
	Environment string   `yaml:"environment" validate:"required"`
	Server      Server   `yaml:"server"`
	DB          Database `yaml:"database"`
	AWSSQS      AWSSQS   `yaml:"awssqs"`
	JWT         JWT      `yaml:"jwt"`
}

type AWSSQS struct {
	Region          string `yaml:"region" validate:"required"`
	AccessKeyID     string `yaml:"access_key_id" validate:"required"`
	SecretAccessKey string `yaml:"secret_access_key" validate:"required"`
}

type JWT struct {
	SecretKey            string `yaml:"secret_key" validate:"required"`
	Issuer               string `yaml:"issuer" validate:"required"`
	Audience             string `yaml:"audience" validate:"required"`
	AccessTokenDuration  string `yaml:"access_token_duration" validate:"required"`
	RefreshTokenDuration string `yaml:"refresh_token_duration" validate:"required"`
}

func LoadConfig() (*Config, error) {
	return LoadConfigFile("app.yaml")
}

func LoadConfigFile(path string) (*Config, error) {
	var config Config

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loadconfig: read %s: %w", path, err)
	}

	reader := strings.NewReader(string(file))
	if err := yaml.NewDecoder(reader).Decode(&config); err != nil {
		return nil, fmt.Errorf("loadconfig: decode %s: %w", path, err)
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
