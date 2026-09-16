package incidentsource

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/scenarioauthoring"
)

func providerSentinels() []string {
	return []string{
		"Authorization: Bearer sentinel-bearer-9f31",
		"api_key=sentinel-apikey-77ab",
		"password=sentinel-password-51cd",
		"ghp_SentinelGithubToken1234567890",
		"AKIAIOSFODNN7EXAMPLE",
		"postgres://svc:sentinel-db-pass-88@db.internal/app",
	}
}

func sentinelPEMBlock() string {
	return "-----BEGIN PRIVATE KEY-----\nprivate-key-body-sentinel-zz99\n-----END PRIVATE KEY-----"
}

func sentinelIncident(provider, id string) NormalizedIncident {
	joined := strings.Join(providerSentinels(), "\n") + "\n" + sentinelPEMBlock()
	return NormalizedIncident{
		Provider:   provider,
		ExternalID: id,
		Title:      "checkout outage " + joined,
		Summary:    "impact summary " + joined,
		Status:     "triggered",
		Severity:   "high",
		Services:   []string{"checkout " + joined},
		Teams:      []string{"sre"},
		Timeline: []TimelineEntry{
			{Type: "note", Summary: "timeline note " + joined, Details: "https://example.com/?token=sentinel-apikey-77ab"},
		},
		Metadata: map[string]string{"tag": joined},
	}
}

func incidentInputFor(incident NormalizedIncident, connectionID, contract string) scenarioauthoring.IncidentInput {
	stored := StoredConnection{Connection: Connection{ID: connectionID, Provider: incident.Provider, ConnectorContract: contract}}
	// incidentInput is the production mapping used before authoring.
	return incidentInput(incident, stored)
}

func assertNoSentinels(t *testing.T, label string, value []byte) {
	t.Helper()
	for _, sentinel := range []string{
		"sentinel-bearer-9f31", "sentinel-apikey-77ab", "sentinel-password-51cd",
		"SentinelGithubToken1234567890", "AKIAIOSFODNN7EXAMPLE", "sentinel-db-pass-88",
		"private-key-body-sentinel-zz99",
	} {
		if strings.Contains(string(value), sentinel) {
			t.Fatalf("%s leaked sentinel %q in %s", label, sentinel, value)
		}
	}
}

func TestProviderImportSentinelRedaction(t *testing.T) {
	for _, provider := range []string{ProviderPagerDuty, ProviderDatadog} {
		incident := sentinelIncident(provider, "ext-123")
		input := incidentInputFor(incident, "conn-1", provider+"-incidents:v1")
		redacted, err := scenarioauthoring.NormalizeIncident(input)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{
			"description": redacted.Description,
			"timeline":    redacted.Timeline,
			"facts":       redacted.Facts,
			"evidence":    redacted.SanitizedEvidence,
			"summary":     redacted.EvidenceSummary,
		})
		assertNoSentinels(t, provider+" redacted authoring input", payload)
		if !strings.Contains(string(payload), scenarioauthoring.RedactionContract) && len(redacted.SanitizedEvidence) == 0 {
			t.Fatalf("%s lost sanitized evidence", provider)
		}
		safe := safePreview(incident)
		previewPayload, _ := json.Marshal(safe)
		assertNoSentinels(t, provider+" safe preview", previewPayload)
		if safe.Provider != provider || safe.ExternalID != "ext-123" {
			t.Fatalf("%s preview lost provenance identity: %+v", provider, safe)
		}
	}
}

func TestNoRawPayloadPersistence(t *testing.T) {
	rawFixture := `{"incidents":[{"id":"PAAA","secret_token":"raw-provider-secret-xyz","description":"raw body"}],"token":"raw-provider-secret-xyz"}`
	incident := NormalizedIncident{
		Provider:   ProviderPagerDuty,
		ExternalID: "PAAA",
		Title:      "api down",
		Summary:    "latency",
		Services:   []string{"checkout"},
		Timeline:   []TimelineEntry{{Summary: "acked"}},
		Metadata:   map[string]string{"incident_number": "7"},
	}
	encoded, _ := json.Marshal(incident)
	if strings.Contains(string(encoded), "raw_json") || strings.Contains(string(encoded), "raw_body") || strings.Contains(string(encoded), "raw-provider-secret-xyz") {
		t.Fatalf("normalized incident must not carry raw payloads: %s", encoded)
	}
	if strings.Contains(rawFixture, "PAAA") && strings.Contains(string(encoded), "raw-provider-secret-xyz") {
		t.Fatal("raw provider secret reached normalized form")
	}
	input := incidentInputFor(incident, "conn-1", PagerDutyContract)
	if input.Provenance == nil {
		t.Fatal("provenance missing")
	}
	provenancePayload, _ := json.Marshal(input.Provenance)
	for _, forbidden := range []string{"api_token", "api_key", "application_key", "raw-provider-secret-xyz"} {
		if strings.Contains(strings.ToLower(string(provenancePayload)), forbidden) {
			t.Fatalf("provenance leaked %q: %s", forbidden, provenancePayload)
		}
	}
	if input.Provenance.SourceConnectionID != "conn-1" || input.Provenance.ExternalIncidentID != "PAAA" || input.Provenance.ConnectorContractVersion != PagerDutyContract {
		t.Fatalf("bad provenance: %s", provenancePayload)
	}
}
