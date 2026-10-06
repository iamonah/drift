package config

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v4"
)

type Database struct {
	Host            string `yaml:"host"`
	Port            string `yaml:"port"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	Name            string `yaml:"name"`
	SSLMode         string `yaml:"ssl_mode"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime"`
	ConnMaxIdleTime string `yaml:"conn_max_idle_time"`
	MaxConns        int    `yaml:"max_conns"`
	MinConns        int    `yaml:"min_conns"`
}

type Server struct {
	Port         string `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type Config struct {
	Environment string   `yaml:"environment"`
	Server      Server   `yaml:"server"`
	DB          Database `yaml:"database"`
	AWSSQS      AWSSQS   `yaml:"awssqs"`
}

type AWSSQS struct {
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
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
	err = yaml.NewDecoder(reader).Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("loadconfig: decode %s: %w", path, err)
	}

	return &config, nil
}
