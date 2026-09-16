package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func TestHealthz(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	NewRouter(stubPinger{}, testLogger()).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if body := strings.TrimSpace(response.Body.String()); body != `{"status":"ok"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "ready", status: http.StatusOK, body: `{"status":"ready"}`},
		{name: "database unavailable", err: errors.New("unavailable"), status: http.StatusServiceUnavailable, body: `{"status":"not_ready"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()
			NewRouter(stubPinger{err: tt.err}, testLogger()).ServeHTTP(response, request)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			if body := strings.TrimSpace(response.Body.String()); body != tt.body {
				t.Fatalf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
