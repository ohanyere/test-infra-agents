package incidentsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pdRecorder struct {
	methods []string
}

func pdTestServer(t *testing.T, recorder *pdRecorder, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.methods = append(recorder.methods, r.Method+" "+r.URL.Path)
		handler(w, r)
	}))
}

func pdClient(server *httptest.Server) *pagerDutyConnector {
	return newPagerDutyConnectorForTest("test-token-abc", server.URL, server.Client())
}

func TestPagerDutyTestConnectionSuccess(t *testing.T) {
	recorder := &pdRecorder{}
	server := pdTestServer(t, recorder, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token token=test-token-abc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"incidents":[],"limit":1,"offset":0,"more":false}`))
	})
	defer server.Close()
	if err := pdClient(server).TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPagerDutyAuthAndPermissionMapping(t *testing.T) {
	for _, tc := range []struct {
		status string
		code   int
	}{
		{CodePagerDutyAuthenticationFailed, http.StatusUnauthorized},
		{CodePagerDutyPermissionRequired, http.StatusForbidden},
	} {
		recorder := &pdRecorder{}
		server := pdTestServer(t, recorder, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte(`{}`))
		})
		err := pdClient(server).TestConnection(context.Background())
		server.Close()
		if !IsProviderError(err, tc.status) {
			t.Fatalf("expected %s got %v", tc.status, err)
		}
	}
}

func TestPagerDutyTimeout(t *testing.T) {
	recorder := &pdRecorder{}
	server := pdTestServer(t, recorder, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := pdClient(server).TestConnection(ctx)
	if !IsProviderError(err, CodeProviderTimeout) {
		t.Fatalf("expected timeout got %v", err)
	}
}

func TestPagerDutyListBoundedPagination(t *testing.T) {
	recorder := &pdRecorder{}
	server := pdTestServer(t, recorder, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "50" {
			t.Errorf("expected server-controlled bounded limit=50, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"incidents":[
			{"id":"PAAA","title":"api down","description":{"summary":"latency spike"},"status":"triggered","urgency":"high","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T04:00:00Z","service":{"summary":"checkout"},"teams":[{"summary":"sre"}]},
			{"id":"PBBB","title":"db slow","description":"locks","status":"acknowledged","urgency":"low","created_at":"2026-01-03T03:04:05Z","updated_at":"2026-01-03T04:00:00Z","service":{"summary":"ledger"}}
		],"limit":20,"offset":0,"more":true}`))
	})
	defer server.Close()
	page, err := pdClient(server).ListIncidents(context.Background(), ListFilter{Limit: 500, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items got %d", len(page.Items))
	}
	if page.Limit != MaxPageSize {
		t.Fatalf("expected bounded limit %d got %d", MaxPageSize, page.Limit)
	}
	if page.NextOffset == nil {
		t.Fatal("expected next offset for paged results")
	}
	if page.Items[0].Provider != ProviderPagerDuty || page.Items[0].Services[0] != "checkout" {
		t.Fatalf("bad normalization: %+v", page.Items[0])
	}
}

func TestPagerDutyGetIncidentWithTimeline(t *testing.T) {
	recorder := &pdRecorder{}
	server := pdTestServer(t, recorder, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/log_entries"):
			_, _ = w.Write([]byte(`{"log_entries":[{"id":"1","type":"acknowledge","created_at":"2026-01-02T03:10:00Z","summary":"acked by sam"}]}`))
		default:
			_, _ = w.Write([]byte(`{"incident":{"id":"PAAA","title":"api down","description":"timeout","status":"resolved","urgency":"high","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T05:00:00Z","resolved_at":"2026-01-02T05:00:00Z","html_url":"https://example/incidents/PAAA","service":{"summary":"checkout"}}}`))
		}
	})
	defer server.Close()
	incident, err := pdClient(server).GetIncident(context.Background(), "PAAA")
	if err != nil {
		t.Fatal(err)
	}
	if incident.ExternalID != "PAAA" || incident.StartedAt == nil || incident.ResolvedAt == nil {
		t.Fatalf("bad incident mapping: %+v", incident)
	}
	if len(incident.Timeline) != 1 || incident.Timeline[0].Summary != "acked by sam" {
		t.Fatalf("bad timeline: %+v", incident.Timeline)
	}
	for _, call := range recorder.methods {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("pagerDuty adapter must be read-only, saw %s", call)
		}
	}
}

func TestPagerDutyNoWrites(t *testing.T) {
	recorder := &pdRecorder{}
	server := pdTestServer(t, recorder, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"incidents":[],"limit":1,"offset":0,"more":false}`))
	})
	defer server.Close()
	client := pdClient(server)
	_ = client.TestConnection(context.Background())
	_, _ = client.ListIncidents(context.Background(), ListFilter{Limit: 5})
	for _, call := range recorder.methods {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("non-GET provider call: %s", call)
		}
	}
}
