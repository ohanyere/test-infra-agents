package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                         int
	DatabaseURL                  string
	Environment                  string
	LogLevel                     string
	SessionTTL                   time.Duration
	AppOrigin                    string
	CookieSecure                 bool
	AgentCredentialEncryptionKey string
	AllowPrivateAgentEndpoints   bool
}

func Load() (Config, error) {
	databaseURL, err := LoadDatabaseURL()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:        8080,
		DatabaseURL: databaseURL,
		Environment: valueOrDefault("ENVIRONMENT", "development"),
		LogLevel:    strings.ToLower(valueOrDefault("LOG_LEVEL", "info")),
		SessionTTL:  168 * time.Hour,
	}
	cfg.AppOrigin = strings.TrimRight(strings.TrimSpace(os.Getenv("APP_ORIGIN")), "/")
	if cfg.AppOrigin == "" && cfg.Environment == "development" {
		cfg.AppOrigin = "http://localhost:3000"
	}
	cfg.CookieSecure = cfg.Environment != "development"
	cfg.AgentCredentialEncryptionKey = strings.TrimSpace(os.Getenv("AGENT_CREDENTIAL_ENCRYPTION_KEY"))
	if cfg.AgentCredentialEncryptionKey == "" {
		return Config{}, errors.New("AGENT_CREDENTIAL_ENCRYPTION_KEY is required")
	}
	if raw := strings.TrimSpace(os.Getenv("ALLOW_PRIVATE_AGENT_ENDPOINTS")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return Config{}, errors.New("ALLOW_PRIVATE_AGENT_ENDPOINTS must be true or false")
		}
		if value && cfg.Environment != "development" {
			return Config{}, errors.New("ALLOW_PRIVATE_AGENT_ENDPOINTS can be true only in development")
		}
		cfg.AllowPrivateAgentEndpoints = value
	}
	if raw := strings.TrimSpace(os.Getenv("SESSION_TTL")); raw != "" {
		duration, parseErr := time.ParseDuration(raw)
		if parseErr != nil || duration <= 0 {
			return Config{}, errors.New("SESSION_TTL must be a positive duration")
		}
		cfg.SessionTTL = duration
	}
	if raw := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return Config{}, errors.New("COOKIE_SECURE must be true or false")
		}
		if cfg.Environment != "development" && !value {
			return Config{}, errors.New("COOKIE_SECURE cannot be false outside development")
		}
		cfg.CookieSecure = value
	}
	if cfg.AppOrigin == "" {
		return Config{}, errors.New("APP_ORIGIN is required")
	}

	if rawPort := strings.TrimSpace(os.Getenv("PORT")); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
		cfg.Port = port
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, errors.New("LOG_LEVEL must be one of debug, info, warn, or error")
	}

	return cfg, nil
}

func LoadDatabaseURL() (string, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return "", errors.New("DATABASE_URL is required")
	}
	return databaseURL, nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
