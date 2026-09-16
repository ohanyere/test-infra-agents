package main

import (
	"net/http/httptest"
	"testing"
)

func TestCapacityAndHealth(t *testing.T) {
	a := newApp(config{MaxConcurrentWork: 1})
	a.active.Store(1) // One accepted work item occupies the only slot.
	request := httptest.NewRequest("GET", "/work", nil)
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, request)
	if response.Code != 503 || a.active.Load() != 1 {
		t.Fatalf("overflow status=%d active=%d", response.Code, a.active.Load())
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		r := httptest.NewRecorder()
		a.handler().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("health affected: %s", path)
		}
	}
	a.active.Store(0)
	r := httptest.NewRecorder()
	a.handler().ServeHTTP(r, request)
	if r.Code != 200 || a.active.Load() != 0 {
		t.Fatal("capacity did not recover")
	}
	metrics := httptest.NewRecorder()
	a.handler().ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	if metrics.Code != 200 {
		t.Fatal("metrics unavailable")
	}
}

func TestCapacityDefaultUnlimited(t *testing.T) {
	a := newApp(config{})
	a.active.Store(100)
	r := httptest.NewRecorder()
	a.handler().ServeHTTP(r, httptest.NewRequest("GET", "/work", nil))
	if r.Code != 200 {
		t.Fatal("default capacity must remain unlimited")
	}
}
