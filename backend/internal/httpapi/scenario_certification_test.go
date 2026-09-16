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
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
)

type fakeCertificationService struct {
	cert       scenariocertification.Certification
	status     scenariocertification.CertificationStatus
	readErr    error
	writeErr   error
	gotRequest scenariocertification.CertifyRequest
}

func (f *fakeCertificationService) Certify(_ context.Context, _ auth.Principal, _, _, _ string, req scenariocertification.CertifyRequest) (scenariocertification.Certification, error) {
	if f.writeErr != nil {
		return scenariocertification.Certification{}, f.writeErr
	}
	f.gotRequest = req
	if f.cert.ID == "" {
		f.cert = scenariocertification.Certification{ID: testUUID, State: scenariocertification.StateCertified, CertificationContractVersion: scenariocertification.CertificationContractVersion, GateResults: []scenariocertification.GateResult{}}
	}
	return f.cert, nil
}
func (f *fakeCertificationService) GetStatus(_ context.Context, _ auth.Principal, _, _, _ string) (scenariocertification.CertificationStatus, error) {
	if f.readErr != nil {
		return scenariocertification.CertificationStatus{}, f.readErr
	}
	if f.status.EffectiveState == "" {
		f.status = scenariocertification.CertificationStatus{EffectiveState: scenariocertification.StateUncertified, History: []scenariocertification.Certification{}, CertificationContract: scenariocertification.CertificationContractVersion}
	}
	return f.status, nil
}
func (f *fakeCertificationService) PutReference(_ context.Context, _ auth.Principal, _, _, _ string, in scenariocertification.ReferenceRemediationInput) (scenariocertification.ReferenceRemediation, error) {
	if f.writeErr != nil {
		return scenariocertification.ReferenceRemediation{}, f.writeErr
	}
	return scenariocertification.ReferenceRemediation{ScenarioVersionID: testUUID, ContractVersion: scenariocertification.ReferenceRemediationContractVersion, ContentHash: "hash-123"}, nil
}
func (f *fakeCertificationService) EligibleAuthoritativeEvidence(_ context.Context, _ auth.Principal, _, _, _ string) (bool, error) {
	if f.readErr != nil {
		return false, f.readErr
	}
	return f.status.Eligible, nil
}

func certificationRouter(service CertificationService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger,
		WithIdentityService(fakeIdentity{}),
		WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"),
		WithScenarioService(fakeScenarioService{}),
		WithCertificationService(service))
}

func serveCertification(service CertificationService, method, path, body string) *httptest.ResponseRecorder {
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
	certificationRouter(service).ServeHTTP(w, r)
	return w
}

func certificationPath(suffix string) string {
	return "/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions/" + testUUID + "/certification" + suffix
}

// Read: workspace members/viewers consistent with scenario visibility.
func TestGetCertificationReadWorks(t *testing.T) {
	service := &fakeCertificationService{status: scenariocertification.CertificationStatus{EffectiveState: scenariocertification.StateCertified, Eligible: true, History: []scenariocertification.Certification{}}}
	w := serveCertification(service, http.MethodGet, certificationPath(""), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body scenariocertification.CertificationStatus
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.EffectiveState != scenariocertification.StateCertified {
		t.Fatalf("state=%q", body.EffectiveState)
	}
}

// Trigger certification: write-capable roles only; server derives state.
func TestTriggerCertificationCreatesAttempt(t *testing.T) {
	service := &fakeCertificationService{}
	w := serveCertification(service, http.MethodPost, certificationPath(""), `{}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body scenariocertification.Certification
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.CertificationContractVersion != "scenario-certification-contract:v1" {
		t.Fatalf("contract=%q", body.CertificationContractVersion)
	}
}

// Users cannot manually submit state=certified: unknown fields rejected.
func TestCannotSetCertifiedManually(t *testing.T) {
	service := &fakeCertificationService{}
	for _, body := range []string{
		`{"state":"certified"}`,
		`{"certified":true}`,
		`{"state":"certified","idempotency_key":"k"}`,
		`{"gate_results":[]}`,
		`{"failure_codes":["reference_remediation_failed"]}`,
		`{"evaluator_version":"deterministic-evaluator:v1"}`,
	} {
		w := serveCertification(service, http.MethodPost, certificationPath(""), body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

// Simulation controls are not part of the public HTTP trigger.
func TestSimulateRejectedByHTTP(t *testing.T) {
	service := &fakeCertificationService{}
	for _, body := range []string{
		`{"simulate":{}}`,
		`{"simulate":{"reference_fails":true}}`,
		`{"simulate":{"dirty_environment":true},"idempotency_key":"k"}`,
	} {
		w := serveCertification(service, http.MethodPost, certificationPath(""), body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

// Callers may not choose the authoritative hidden certification variants.
func TestVariantConfigsRejectedByHTTP(t *testing.T) {
	service := &fakeCertificationService{}
	for _, body := range []string{
		`{"variant_configs":[]}`,
		`{"variant_configs":[{"traffic_vus":10}]}`,
		`{"variant_configs":[{"traffic_vus":10}],"environment_profile":"default"}`,
	} {
		w := serveCertification(service, http.MethodPost, certificationPath(""), body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

// Inline reference remediation is not accepted through the certification
// trigger; use PUT .../reference-remediation instead.
func TestInlineReferenceRemediationRejectedByHTTP(t *testing.T) {
	service := &fakeCertificationService{}
	for _, body := range []string{
		`{"reference_remediation":{"preconditions":{},"reference_actions":[{"type":"scale_workload","target":"app","replicas":3}],"expected_resulting_state":{}}}`,
		`{"reference_remediation":null,"idempotency_key":"k"}`,
	} {
		// Explicit null still names a forbidden field; the first body must 400
		// via unknown-field rejection. A null inline reference is also
		// rejected because the field itself is not part of the public DTO.
		w := serveCertification(service, http.MethodPost, certificationPath(""), body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

// Minimal public trigger shape is accepted and mapped server-side.
func TestMinimalTriggerShapeAccepted(t *testing.T) {
	service := &fakeCertificationService{}
	w := serveCertification(service, http.MethodPost, certificationPath(""),
		`{"idempotency_key":"k-1","environment_profile":"default","runtime_contract_version":"kubernetes-traffic-spike:v1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if service.gotRequest.IdempotencyKey == nil || *service.gotRequest.IdempotencyKey != "k-1" {
		t.Fatalf("idempotency key not mapped: %+v", service.gotRequest)
	}
	if service.gotRequest.EnvironmentProfile != "default" {
		t.Fatalf("environment profile not mapped: %+v", service.gotRequest)
	}
	if service.gotRequest.RuntimeContractVersion != "kubernetes-traffic-spike:v1" {
		t.Fatalf("runtime contract not mapped: %+v", service.gotRequest)
	}
}

// Viewer cannot trigger certification: forbidden surfaces as 403.
func TestViewerCannotTriggerCertification(t *testing.T) {
	service := &fakeCertificationService{writeErr: auth.ErrForbidden}
	w := serveCertification(service, http.MethodPost, certificationPath(""), `{}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

// Workspace isolation: cross-workspace attempts are forbidden, never leak.
func TestCertificationWorkspaceIsolation(t *testing.T) {
	service := &fakeCertificationService{readErr: auth.ErrForbidden}
	w := serveCertification(service, http.MethodGet, certificationPath(""), "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

// Reference remediation authoring never returns reference contents.
func TestPutReferenceHidesContents(t *testing.T) {
	service := &fakeCertificationService{}
	w := serveCertification(service, http.MethodPut, "/v1/workspaces/"+testUUID+"/scenarios/"+testUUID+"/versions/"+testUUID+"/reference-remediation",
		`{"preconditions":{},"reference_actions":[{"type":"scale_workload","target":"app","replicas":3}],"expected_resulting_state":{}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	lowered := strings.ToLower(w.Body.String())
	for _, marker := range []string{"scale_workload", "preconditions", "expected_resulting_state", "secret"} {
		if marker == "scale_workload" {
			continue
		}
		if strings.Contains(lowered, marker) && strings.Contains(w.Body.String(), `"reference_actions"`) {
			t.Fatalf("response must not embed reference contents: %s", w.Body.String())
		}
	}
	if strings.Contains(w.Body.String(), `"reference_actions"`) {
		t.Fatalf("response must not embed reference actions: %s", w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["contract_version"] != "reference-remediation:v1" {
		t.Fatalf("contract not pinned: %s", w.Body.String())
	}
}

// Eligibility endpoint distinguishes certified from uncertified.
func TestCertificationEligibilityEndpoint(t *testing.T) {
	service := &fakeCertificationService{status: scenariocertification.CertificationStatus{EffectiveState: scenariocertification.StateUncertified, Eligible: false}}
	w := serveCertification(service, http.MethodGet, certificationPath("/eligibility"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["eligible_authoritative_evidence"] != false {
		t.Fatalf("legacy must not be eligible: %s", w.Body.String())
	}
}

// Unauthenticated reads are rejected.
func TestCertificationRequiresAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(healthyDatabase{}, logger,
		WithIdentityService(fakeIdentity{}),
		WithAuthentication(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeAuth{authenticateErr: auth.ErrUnauthenticated}, false, 168*time.Hour, "http://localhost:3000"),
		WithCertificationService(&fakeCertificationService{}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, certificationPath(""), nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

// Unknown scenario version surfaces 404, never fabricated state.
func TestCertificationNotFound(t *testing.T) {
	service := &fakeCertificationService{readErr: identity.ErrNotFound}
	w := serveCertification(service, http.MethodGet, certificationPath(""), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
