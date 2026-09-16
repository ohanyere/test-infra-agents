package config

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want DATABASE_URL validation error")
	}
}

func setCredentialKey(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_CREDENTIAL_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestLoadDefaults(t *testing.T) {
	setCredentialKey(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("PORT", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("AGENT_CREDENTIAL_ENCRYPTION_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("ALLOW_PRIVATE_AGENT_ENDPOINTS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != 8080 || cfg.Environment != "development" || cfg.LogLevel != "info" {
		t.Fatalf("Load() defaults = %+v", cfg)
	}
	if cfg.SessionTTL != 168*time.Hour || cfg.AppOrigin != "http://localhost:3000" || cfg.CookieSecure {
		t.Fatalf("Load() auth defaults = %+v", cfg)
	}
}

func TestLoadRejectsInsecureProductionCookie(t *testing.T) {
	setCredentialKey(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("AGENT_CREDENTIAL_ENCRYPTION_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted insecure production cookie")
	}
}

func TestLoadRejectsPrivateEndpointsOutsideDevelopment(t *testing.T) {
	setCredentialKey(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("APP_ORIGIN", "https://app.example")
	t.Setenv("ALLOW_PRIVATE_AGENT_ENDPOINTS", "true")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted private endpoint access in production")
	}
}
