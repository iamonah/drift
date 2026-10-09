package config

import (
	"fmt"
	"os"
	"strings"

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
}

type AWSSQS struct {
	Region          string `yaml:"region" validate:"required"`
	AccessKeyID     string `yaml:"access_key_id" validate:"required"`
	SecretAccessKey string `yaml:"secret_access_key" validate:"required"`
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

	return &config, nil
}
