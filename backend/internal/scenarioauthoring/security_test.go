package scenarioauthoring

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type allowAuthoringRole struct{}

func (allowAuthoringRole) GetRole(context.Context, identity.DBTX, string, string) (string, error) {
	return "member", nil
}

type capturingIncidentGenerator struct {
	input IncidentInput
	draft ScenarioDraft
	err   error
}

func (g *capturingIncidentGenerator) FromDescription(context.Context, DescriptionInput) (ScenarioDraft, error) {
	return g.draft, g.err
}

func (g *capturingIncidentGenerator) FromIncident(_ context.Context, input IncidentInput) (ScenarioDraft, error) {
	g.input = input
	return g.draft, g.err
}

func securityIncident() IncidentInput {
	return IncidentInput{
		Description: "checkout incident password=SUPER_SECRET_DB_PASSWORD_9fd1",
		Timeline:    "HTTP 500 Authorization: Bearer sentinel-token-123 at namespace payments",
		Files: []EvidenceFile{
			{Name: "secret.yaml", Content: []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: checkout-secret\n  namespace: payments\nstringData:\n  password: kube-string-secret\n")},
			{Name: "incident.log", Content: []byte("postgres://user:database-url-secret@db.internal/app\n-----BEGIN PRIVATE KEY-----\nprivate-key-sentinel\n-----END PRIVATE KEY-----\nrequest_rate=150")},
		},
	}
}

func securitySentinels() []string {
	return []string{"SUPER_SECRET_DB_PASSWORD_9fd1", "sentinel-token-123", "kube-string-secret", "database-url-secret", "private-key-sentinel"}
}

func assertNoSecuritySentinels(t *testing.T, value []byte) {
	t.Helper()
	for _, sentinel := range securitySentinels() {
		if strings.Contains(string(value), sentinel) {
			t.Fatalf("secret %q leaked in %s", sentinel, value)
		}
	}
}

func TestIncidentProviderBoundaryReceivesOnlyRedactedRepresentation(t *testing.T) {
	draft := validDraft()
	generator := &capturingIncidentGenerator{draft: draft}
	service := NewService(generator, nil, allowAuthoringRole{})
	result, err := service.ImportIncident(context.Background(), auth.Principal{UserID: "user-1"}, "workspace-1", securityIncident())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(generator.input)
	assertNoSecuritySentinels(t, payload)
	if len(generator.input.Files) != 0 {
		t.Fatal("provider received raw incident files")
	}
	if !strings.Contains(string(payload), "checkout") || !strings.Contains(string(payload), "payments") || !strings.Contains(string(payload), "request_rate=150") {
		t.Fatalf("safe operational context did not reach provider: %s", payload)
	}
	metadata := string(result.Draft.SourceMetadata)
	if !strings.Contains(metadata, RedactionContract) {
		t.Fatalf("redaction provenance missing: %s", metadata)
	}
}

func TestActualOpenAIRequestContainsNoIncidentSecrets(t *testing.T) {
	var outbound []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		outbound, _ = io.ReadAll(request.Body)
		http.Error(w, "rejected for test", http.StatusBadRequest)
	}))
	defer server.Close()
	generator, err := NewOpenAIGenerator(OpenAIConfig{APIKey: "test-key", Model: "test-model", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(generator, nil, allowAuthoringRole{})
	_, err = service.ImportIncident(context.Background(), auth.Principal{UserID: "user-1"}, "workspace-1", securityIncident())
	if err == nil {
		t.Fatal("expected provider test response to fail")
	}
	assertNoSecuritySentinels(t, outbound)
	if !strings.Contains(string(outbound), "checkout") || !strings.Contains(string(outbound), "payments") {
		t.Fatalf("provider payload lost safe operational context: %s", outbound)
	}
}

func TestProviderDraftAndVersionCannotCopyIncidentSecrets(t *testing.T) {
	draft := validDraft()
	secret := "SUPER_SECRET_DB_PASSWORD_9fd1"
	draft.Description = "copied " + secret
	draft.AgentSpec.Task = "investigate " + secret
	draft.AgentSpec.VisibleContext = json.RawMessage(`{"note":"SUPER_SECRET_DB_PASSWORD_9fd1","token":"sentinel-token-123"}`)
	draft.Conditions = json.RawMessage(`[{"message":"database-url-secret"}]`)
	draft.ExecutionSpec = json.RawMessage(`{"type":"traffic_spike","password":"kube-string-secret"}`)
	draft.Assumptions = []string{"private-key-sentinel"}
	draft.Warnings = []string{secret}
	draft.SourceMetadata = json.RawMessage(`{"provider_note":"sentinel-token-123"}`)
	generator := &capturingIncidentGenerator{draft: draft}
	service := NewService(generator, nil, allowAuthoringRole{})
	result, err := service.ImportIncident(context.Background(), auth.Principal{UserID: "user-1"}, "workspace-1", securityIncident())
	if err != nil {
		t.Fatal(err)
	}
	draftJSON, _ := json.Marshal(result)
	assertNoSecuritySentinels(t, draftJSON)
	versionJSON, _ := json.Marshal(result.Draft.VersionInput("incident_import"))
	assertNoSecuritySentinels(t, versionJSON)
}

func TestProviderErrorsDoNotReturnIncidentSecrets(t *testing.T) {
	sentinel := "SUPER_SECRET_DB_PASSWORD_9fd1"
	generator := &capturingIncidentGenerator{err: errors.New("provider rejected " + sentinel)}
	service := NewService(generator, nil, allowAuthoringRole{})
	_, err := service.ImportIncident(context.Background(), auth.Principal{UserID: "user-1"}, "workspace-1", securityIncident())
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("unsafe provider error: %v", err)
	}
}
