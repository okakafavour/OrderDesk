package config

import (
	"fmt"
	"os"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string
}

func Load() (Config, error) {
	cfg := Config{
		DBHost:     os.Getenv("DB_HOST"),
		DBPort:     os.Getenv("DB_PORT"),
		DBUser:     os.Getenv("DB_USER"),
		DBPassword: os.Getenv("DB_PASSWORD"),
		DBName:     os.Getenv("DB_NAME"),
		DBSSLMode:  os.Getenv("DB_SSLMODE"),
	}

	if cfg.DBPort == "" {
		cfg.DBPort = "5432"
	}

	if cfg.DBSSLMode == "" {
		cfg.DBSSLMode = "disable"
	}

	if cfg.DBHost == "" ||
		cfg.DBUser == "" ||
		cfg.DBPassword == "" ||
		cfg.DBName == "" {
		return Config{}, fmt.Errorf(
			"required database configuration is missing",
		)
	}

	return cfg, nil
}
