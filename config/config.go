package config

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

func logEnvLoading(envFile string, err error) bool {
	if err != nil {
		slog.Info("skipping env file", "fileName", envFile, "err", err)
	} else {
		slog.Info(fmt.Sprintf("loading: %s", envFile))
	}
	return err == nil
}

func LoadEnv(config any) any {
	goEnv := os.Getenv("WWW_GO_ENV")
	if goEnv == "" {
		goEnv = "development"
	}
	envFile := ""

	var loaded bool
	appName := os.Getenv("APP_NAME")
	envFile = fmt.Sprintf(".env.%s.%s.local", appName, goEnv)
	loaded = logEnvLoading(envFile, godotenv.Load(envFile))

	if !loaded {
		envFile = fmt.Sprintf(".env.%s.%s", appName, goEnv)
		loaded = logEnvLoading(envFile, godotenv.Load(envFile))
	}

	if !loaded {
		envFile = fmt.Sprintf(".env.%s", appName)
		loaded = logEnvLoading(envFile, godotenv.Load(envFile))
	}

	if !loaded {
		envFile = fmt.Sprintf(".env.%s.local", goEnv)
		loaded = logEnvLoading(envFile, godotenv.Load(envFile))
	}

	if !loaded {
		envFile = fmt.Sprintf(".env.%s", goEnv)
		loaded = logEnvLoading(envFile, godotenv.Load(envFile))
	}

	var err error
	if !loaded {
		err = godotenv.Load() // reads default .env
		if err != nil {
			slog.Info("skipping env file", "fileName", envFile, "err", err)
			slog.Info("falling back to exported goEnvironment variables")
		} else {
			slog.Info("loading: .env")
		}
	}

	err = env.Parse(config)
	if err != nil {
		log.Fatalf("failed to parse env vars: %v", err)
	}

	return config
}
