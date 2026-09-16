package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpointsAndMetrics(t *testing.T) {
	a := newApp(config{Name: "test", Version: "v1"})
	s := httptest.NewServer(a.handler())
	defer s.Close()
	for _, path := range []string{"/healthz", "/readyz", "/", "/work"} {
		r, e := http.Get(s.URL + path)
		if e != nil || r.StatusCode != 200 {
			t.Fatalf("%s: %v %v", path, r, e)
		}
		r.Body.Close()
	}
	r, _ := http.Get(s.URL + "/metrics")
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	text := string(b)
	if !strings.Contains(text, "http_requests_total") || !strings.Contains(text, "http_request_duration_seconds") {
		t.Fatal("application metrics missing")
	}
}
func TestControlledLatencyAndErrorsDoNotAffectHealth(t *testing.T) {
	a := newApp(config{Latency: 20 * time.Millisecond, ErrorRate: 100, Name: "test"})
	s := httptest.NewServer(a.handler())
	defer s.Close()
	start := time.Now()
	r, _ := http.Get(s.URL + "/work")
	r.Body.Close()
	if r.StatusCode != 503 || time.Since(start) < 18*time.Millisecond {
		t.Fatalf("status=%d duration=%v", r.StatusCode, time.Since(start))
	}
	r, _ = http.Get(s.URL + "/healthz")
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("health affected by workload injection")
	}
}
