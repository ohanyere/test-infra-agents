package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type config struct {
	MaxConcurrentWork int64
	Latency           time.Duration
	ErrorRate         float64
	Name, Version     string
}

func loadConfig() (config, error) {
	c := config{Name: "eval-reference-app", Version: "dev"}
	if v := os.Getenv("MAX_CONCURRENT_WORK"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return c, errors.New("MAX_CONCURRENT_WORK must be a non-negative integer")
		}
		c.MaxConcurrentWork = n
	}
	if v := os.Getenv("APP_NAME"); v != "" {
		c.Name = v
	}
	if v := os.Getenv("APP_VERSION"); v != "" {
		c.Version = v
	}
	if v := os.Getenv("APP_LATENCY_MS"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 {
			return c, errors.New("APP_LATENCY_MS must be a non-negative integer")
		}
		c.Latency = time.Duration(n) * time.Millisecond
	}
	if v := os.Getenv("APP_ERROR_RATE"); v != "" {
		n, e := strconv.ParseFloat(v, 64)
		if e != nil || n < 0 || n > 100 {
			return c, errors.New("APP_ERROR_RATE must be a percentage from 0 to 100")
		}
		c.ErrorRate = n
	}
	return c, nil
}

type app struct {
	active   atomic.Int64
	config   config
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func newApp(c config) *app {
	r := prometheus.NewRegistry()
	a := &app{config: c, registry: r, requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total", Help: "Total bounded-route HTTP requests."}, []string{"method", "route", "status_class"}), duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}), inFlight: prometheus.NewGauge(prometheus.GaugeOpts{Name: "http_in_flight_requests", Help: "Current workload requests."})}
	info := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "application_info", Help: "Reference application identity."}, []string{"name", "version"})
	info.WithLabelValues(c.Name, c.Version).Set(1)
	r.MustRegister(a.requests, a.duration, a.inFlight, info)
	return a
}
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(a.registry, promhttp.HandlerOpts{}))
	mux.Handle("GET /", a.instrument("root", a.work))
	mux.Handle("GET /work", a.instrument("work", a.work))
	return mux
}
func (a *app) work(w http.ResponseWriter, _ *http.Request) {
	// Optional portable saturation: accepted work occupies a per-pod slot.
	// Rejected work still incurs configured latency, bounding rejection traffic.
	accepted := true
	if a.config.MaxConcurrentWork > 0 {
		accepted = a.active.Add(1) <= a.config.MaxConcurrentWork
		if accepted {
			defer a.active.Add(-1)
		} else {
			a.active.Add(-1)
		}
	}
	if a.config.Latency > 0 {
		time.Sleep(a.config.Latency)
	}
	w.Header().Set("Content-Type", "application/json")
	if !accepted || (a.config.ErrorRate > 0 && rand.Float64()*100 < a.config.ErrorRate) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": a.config.Name, "version": a.config.Version})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (a *app) instrument(route string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		a.inFlight.Inc()
		defer a.inFlight.Dec()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next(sw, r)
		class := fmt.Sprintf("%dxx", sw.status/100)
		a.requests.WithLabelValues(r.Method, route, class).Inc()
		a.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}
