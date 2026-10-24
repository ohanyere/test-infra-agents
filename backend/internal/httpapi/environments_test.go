package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type fakeEnvironmentService struct{}

func (f *fakeEnvironmentService) Components() []environment.Component {
	return environment.ListComponents()
}

func (f *fakeEnvironmentService) Profiles() []environment.Profile {
	return environment.ListProfiles()
}

func (f *fakeEnvironmentService) Providers() []environment.ProviderInfo {
	return environment.ListProviders()
}

func (f *fakeEnvironmentService) ProviderHealth(_ context.Context, providerID string) (environment.ProviderHealth, error) {
	if _, ok := environment.GetProvider(providerID); !ok {
		return environment.ProviderHealth{}, identity.ErrNotFound
	}
	return environment.DefaultAvailability().Health(context.Background(), providerID), nil
}

func (f *fakeEnvironmentService) SetRequirements(_ context.Context, _ auth.Principal, _, _, _ string, in environment.Requirements) (environment.Requirements, error) {
	normalized, err := environment.ValidateRequirements(in)
	if err != nil {
		return environment.Requirements{}, identity.ErrValidation
	}
	return normalized, nil
}

func (f *fakeEnvironmentService) GetRequirements(_ context.Context, _ auth.Principal, _, _, _ string) (environment.Requirements, bool, error) {
	return environment.LegacyDefaultRequirements(), false, nil
}

func (f *fakeEnvironmentService) Resolve(_ context.Context, _ auth.Principal, _ string, req environment.ResolveRequest) (environment.SchedulerDecision, environment.Requirements, error) {
	if req.ScenarioID == "" || req.ScenarioVersionID == "" {
		return environment.SchedulerDecision{}, environment.Requirements{}, identity.ErrValidation
	}
	requirements := environment.LegacyDefaultRequirements()
	decision := environment.SelectForRequirements(context.Background(), requirements, "trial", req.ProfilePreference, nil)
	return decision, requirements, nil
}

func (f *fakeEnvironmentService) GetInstance(_ context.Context, _ auth.Principal, _, _ string) (environment.EnvironmentInstance, error) {
	return environment.EnvironmentInstance{}, identity.ErrNotFound
}

func (f *fakeEnvironmentService) ListInstances(_ context.Context, _ auth.Principal, _ string, _ int) ([]environment.EnvironmentInstance, error) {
	return []environment.EnvironmentInstance{}, nil
}

func (f *fakeEnvironmentService) ListInstanceComponents(_ context.Context, _ auth.Principal, _, _ string) ([]environment.EnvironmentComponentInstance, error) {
	return []environment.EnvironmentComponentInstance{}, nil
}

func (f *fakeEnvironmentService) TransitionInstance(_ context.Context, _ auth.Principal, _, _, _ string) (environment.EnvironmentInstance, error) {
	return environment.EnvironmentInstance{}, identity.ErrValidation
}

func environmentRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger,
		WithIdentityService(fakeIdentity{}),
		WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"),
		WithScenarioService(fakeScenarioService{}),
		WithEnvironmentService(&fakeEnvironmentService{}))
}

func serveEnvironment(method, path, body string) *httptest.ResponseRecorder {
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
	environmentRouter().ServeHTTP(w, r)
	return w
}

func TestListEnvironmentComponents(t *testing.T) {
	w := serveEnvironment(http.MethodGet, "/v1/environment-components", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body listResponse[environment.Component]
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) < 6 {
		t.Fatalf("expected at least 6 components, got %d", len(body.Items))
	}
}

func TestLegacyEnvironmentComponentsMayBeEmpty(t *testing.T) {
	w := serveEnvironment(http.MethodGet, "/v1/workspaces/11111111-1111-4111-8111-111111111111/environments/22222222-2222-4222-8222-222222222222/components", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body listResponse[environment.EnvironmentComponentInstance]
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("legacy environment returned children: %#v", body.Items)
	}
}

func TestListEnvironmentProfiles(t *testing.T) {
	w := serveEnvironment(http.MethodGet, "/v1/environment-profiles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body listResponse[environment.Profile]
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 4 {
		t.Fatalf("expected 4 profiles, got %d", len(body.Items))
	}
	active := 0
	for _, p := range body.Items {
		if p.Status == environment.ProfileActive {
			active++
			if p.ID != environment.ProfileKubernetesNamespace {
				t.Fatalf("only kubernetes.namespace:v1 may be active, got %s", p.ID)
			}
		}
	}
	if active != 1 {
		t.Fatalf("exactly one profile must be active, got %d", active)
	}
}

func TestListEnvironmentProviders(t *testing.T) {
	w := serveEnvironment(http.MethodGet, "/v1/environment-providers", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body listResponse[environment.ProviderInfo]
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, p := range body.Items {
		if p.ID == environment.ProviderKindDev && (p.Production || !p.DevelopmentOnly) {
			t.Fatalf("kind-dev must be development-only over HTTP")
		}
	}
}

func TestResolveSelectionRequiresScenarioReference(t *testing.T) {
	w := serveEnvironment(http.MethodPost, "/v1/workspaces/"+testUUID+"/environment-selection/resolve",
		`{"scenario_id":"`+testUUID+`","scenario_version_id":"`+testUUID+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["decision"]; !ok {
		t.Fatalf("resolve must return a scheduler decision")
	}
}

func TestPutRequirementsRejectsUnknownComponent(t *testing.T) {
	w := serveEnvironment(http.MethodPut, "/v1/workspaces/"+testUUID+"/scenarios/"+testUUID+"/versions/"+testUUID+"/environment",
		`{"required_components":["aws"],"minimum_isolation_tier":"namespace"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown component must be rejected: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGetVersionEnvironmentShowsEligibleProfile(t *testing.T) {
	w := serveEnvironment(http.MethodGet, "/v1/workspaces/"+testUUID+"/scenarios/"+testUUID+"/versions/"+testUUID+"/environment", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	profile, ok := body["eligible_profile"].(map[string]any)
	if !ok {
		t.Fatalf("version environment must include eligible profile: %s", w.Body.String())
	}
	if profile["id"] != environment.ProfileKubernetesNamespace {
		t.Fatalf("legacy version must be eligible for kubernetes.namespace:v1, got %v", profile["id"])
	}
}
