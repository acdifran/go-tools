package config

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

func LoadEnv(config any) any {
	goEnv := os.Getenv("WWW_GO_ENV")
	if goEnv == "" {
		goEnv = "development"
	}

	err := godotenv.Load(".env." + goEnv + ".local")
	if err != nil {
		slog.Info(fmt.Errorf("skipping .env.%s.local: %w", goEnv, err).Error())
	} else {
		slog.Info("loading: .env." + goEnv + ".local")
	}

	if goEnv != "test" {
		err = godotenv.Load(".env.local")
		if err != nil {
			slog.Info(fmt.Errorf("skipping .env.local: %w", err).Error())
		} else {
			slog.Info("loading: .env.local")
		}
	}

	err = godotenv.Load(".env." + goEnv)
	if err != nil {
		slog.Info(fmt.Errorf("skipping .env.%s: %w", goEnv, err).Error())
	} else {
		slog.Info("loading: .env." + goEnv)
	}

	err = godotenv.Load() // reads default .env
	if err != nil {
		slog.Info(fmt.Errorf("skipping .env: %w", err).Error())
		slog.Info("falling back to exported goEnvironment variables")
	} else {
		slog.Info("loading: .env")
	}

	err = env.Parse(config)
	if err != nil {
		log.Fatalf("failed to parse env vars: %v", err)
	}

	return config
}
