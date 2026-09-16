package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
)

type requestIDContextKey struct{}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if len(id) == 0 || len(id) > 128 || strings.ContainsAny(id, "\r\n\t") {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "request-id-unavailable"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}
func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}

func requestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("HTTP request", "request_id", requestID(r.Context()), "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(started))
		})
	}
}

func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut {
			media := r.Header.Get("Content-Type")
			if strings.HasPrefix(strings.ToLower(media), "multipart/form-data") && strings.HasSuffix(r.URL.Path, "/scenario-drafts/import-incident") {
				next.ServeHTTP(w, r)
				return
			}
			if !strings.HasPrefix(strings.ToLower(media), "application/json") {
				writeAPIError(w, http.StatusUnsupportedMediaType, "validation_error", "Content-Type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type sessionAuthenticator interface {
	Authenticate(context.Context, string) (auth.Principal, error)
}

func authenticationMiddleware(service sessionAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(auth.CookieName)
			if err != nil {
				writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			principal, err := service.Authenticate(r.Context(), cookie.Value)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithPrincipal(r.Context(), principal)))
		})
	}
}
func originMiddleware(appOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if origin := r.Header.Get("Origin"); origin != "" && origin != appOrigin {
					writeAPIError(w, http.StatusForbidden, "forbidden", "request origin is not allowed")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(appOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != appOrigin {
				writeAPIError(w, http.StatusForbidden, "forbidden", "request origin is not allowed")
				return
			}
			allowed := origin != "" && origin == appOrigin
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", appOrigin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				if !allowed {
					writeAPIError(w, http.StatusForbidden, "forbidden", "request origin is not allowed")
					return
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
