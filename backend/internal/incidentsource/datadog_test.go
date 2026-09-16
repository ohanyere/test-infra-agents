package incidentsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ddTestServer(t *testing.T, methods *[]string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("DD-API-KEY") == "" || r.Header.Get("DD-APPLICATION-KEY") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler(w, r)
	}))
}

func ddClient(site string, server *httptest.Server) *datadogConnector {
	client, err := newDatadogConnectorForTest(site, "api-key", "app-key", server.URL, server.Client())
	if err != nil {
		panic(err)
	}
	return client
}

func TestDatadogTestConnectionSuccess(t *testing.T) {
	var methods []string
	server := ddTestServer(t, &methods, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"meta":{"page":{"total_count":0}}}`))
	})
	defer server.Close()
	if err := ddClient("DATADOGHQ.COM", server).TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDatadogAuthPermissionAndSiteMapping(t *testing.T) {
	var methods []string
	server := ddTestServer(t, &methods, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{}`))
	})
	defer server.Close()
	if err := ddClient("DATADOGHQ.COM", server).TestConnection(context.Background()); !IsProviderError(err, CodeDatadogPermissionRequired) {
		t.Fatalf("expected permission error got %v", err)
	}
	if _, err := NewDatadogConnector("EXAMPLE.COM", "k", "a"); !IsProviderError(err, CodeDatadogInvalidSite) {
		t.Fatalf("expected invalid site got %v", err)
	}
	unauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer unauth.Close()
	unauthClient, _ := newDatadogConnectorForTest("DATADOGHQ.COM", "k", "a", unauth.URL, unauth.Client())
	if err := unauthClient.TestConnection(context.Background()); !IsProviderError(err, CodeDatadogAuthenticationFailed) {
		t.Fatalf("expected auth error got %v", err)
	}
}

func TestDatadogTimeout(t *testing.T) {
	var methods []string
	server := ddTestServer(t, &methods, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := ddClient("DATADOGHQ.COM", server).TestConnection(ctx); !IsProviderError(err, CodeProviderTimeout) {
		t.Fatalf("expected timeout got %v", err)
	}
}

func TestDatadogListBoundedPagination(t *testing.T) {
	var methods []string
	server := ddTestServer(t, &methods, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"dd-1","type":"incidents","attributes":{"title":"latency","customer_impact_summary":"p99 high","state":"active","severity":"SEV-2","created":"2026-02-01T01:00:00Z","modified":"2026-02-01T02:00:00Z","tags":["env:prod"]}},
			{"id":"dd-2","type":"incidents","attributes":{"title":"errors","customer_impact_summary":"5xx","state":"resolved","created":"2026-02-02T01:00:00Z","modified":"2026-02-02T02:00:00Z","resolved":"2026-02-02T03:00:00Z"}}
		],"meta":{"page":{"total_count":4}}}`))
	})
	defer server.Close()
	page, err := ddClient("DATADOGHQ.COM", server).ListIncidents(context.Background(), ListFilter{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Limit != MaxPageSize {
		t.Fatalf("bad bounded page: %+v", page)
	}
	if page.NextOffset == nil {
		t.Fatal("expected next offset")
	}
	if page.Items[0].Provider != ProviderDatadog || page.Items[0].Severity != "SEV-2" {
		t.Fatalf("bad normalization: %+v", page.Items[0])
	}
}

func TestDatadogGetIncidentNormalization(t *testing.T) {
	var methods []string
	server := ddTestServer(t, &methods, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/updates") {
			_, _ = w.Write([]byte(`{"data":[{"id":"u1","attributes":{"created":"2026-02-01T01:10:00Z","content":"mitigation started","update_type":"status"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"dd-1","type":"incidents","attributes":{"title":"latency","customer_impact_summary":"p99 high","state":"active","severity":"SEV-2","created":"2026-02-01T01:00:00Z","modified":"2026-02-01T02:00:00Z"}}}`))
	})
	defer server.Close()
	incident, err := ddClient("DATADOGHQ.COM", server).GetIncident(context.Background(), "dd-1")
	if err != nil {
		t.Fatal(err)
	}
	if incident.ExternalID != "dd-1" || len(incident.Timeline) != 1 {
		t.Fatalf("bad datadog incident: %+v", incident)
	}
	for _, call := range methods {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("datadog adapter must be read-only, saw %s", call)
		}
	}
}
