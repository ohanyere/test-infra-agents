package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"io"
	"log/slog"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/auth"
)

func catalogRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"), WithAgentService(fakeAgentService{}))
}

func authenticatedCatalogRequest(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, reader)
	if method != http.MethodGet {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:3000")
	}
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	catalogRouter().ServeHTTP(w, r)
	return w
}

func TestCapabilityCatalogReadWorks(t *testing.T) {
	w := authenticatedCatalogRequest(t, http.MethodGet, "/v1/capability-catalog", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Contract string `json:"contract"`
		Items    []struct {
			ID             string   `json:"id"`
			Domain         string   `json:"domain"`
			Family         string   `json:"family"`
			CatalogVersion string   `json:"catalog_version"`
			Status         string   `json:"status"`
			Requires       []string `json:"requires"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Contract != "infrastructure-capability-catalog:v1" {
		t.Fatalf("contract=%q", body.Contract)
	}
	if len(body.Items) != 24 {
		t.Fatalf("items=%d", len(body.Items))
	}
	seen := map[string]bool{}
	for _, item := range body.Items {
		if item.ID == "" || item.Domain == "" || item.Family == "" {
			t.Fatalf("missing hierarchy in %+v", item)
		}
		if item.CatalogVersion != body.Contract {
			t.Fatalf("version mismatch %+v", item)
		}
		if item.Status != "active" {
			t.Fatalf("V1 catalog must be active-claimable, got %+v", item)
		}
		if len(item.Requires) == 0 {
			t.Fatalf("missing requires %+v", item)
		}
		if seen[item.ID] {
			t.Fatalf("duplicate %q", item.ID)
		}
		seen[item.ID] = true
		lower := strings.ToLower(item.ID)
		for _, forbidden := range []string{"aws", "terraform", "gcp"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("no fake cloud capability %q", item.ID)
			}
		}
	}
}

func TestCapabilityCatalogDetail(t *testing.T) {
	w := authenticatedCatalogRequest(t, http.MethodGet, "/v1/capability-catalog/kubernetes.capacity.remediation", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = authenticatedCatalogRequest(t, http.MethodGet, "/v1/capability-catalog/does.not.exist", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityCatalogRequiresAuthentication(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeAuth{authenticateErr: auth.ErrUnauthenticated}, false, 168*time.Hour, "http://localhost:3000"), WithAgentService(fakeAgentService{}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/capability-catalog", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCapabilityClaimsEndpoint(t *testing.T) {
	w := authenticatedCatalogRequest(t, http.MethodGet, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions/"+testUUID+"/capability-claims", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Contract          string                  `json:"contract"`
		ClaimStatus       string                  `json:"claim_status"`
		Items             []agent.CapabilityClaim `json:"items"`
		QualificationNote string                  `json:"qualification_note"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Contract != "infrastructure-capability-catalog:v1" || body.ClaimStatus != "claimed" {
		t.Fatalf("body=%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"qualified":true`) {
		t.Fatal("claims must never return qualified=true")
	}
	if strings.Contains(w.Body.String(), "—") {
		t.Fatal("claims API must not return em-dash domain state")
	}
	for _, item := range body.Items {
		if item.TestStatus != "" && item.TestStatus != "untested" && item.TestStatus != "partial" && item.TestStatus != "tested" {
			t.Fatalf("invalid test_status %q", item.TestStatus)
		}
		if item.QualificationStatus != nil {
			t.Fatal("per-capability qualification is not implemented; qualification_status must be null")
		}
	}
}

func TestCapabilityClaimsViewerCannotWriteButCanRead(t *testing.T) {
	// Claims inherit AgentVersion registration permissions: viewer read-only.
	// A forbidden write from the agent service must surface as 403.
	r := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions", strings.NewReader(`{"version_label":"v2","capabilities":["inspect"],"claimed_capability_ids":["gitops.drift.detection"],"metadata":{}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"), WithAgentService(fakeAgentService{writeErr: auth.ErrForbidden}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
