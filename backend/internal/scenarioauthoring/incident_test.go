package scenarioauthoring

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeIncidentRemovesSecretsAndExtractsKubernetesFacts(t *testing.T) {
	input := IncidentInput{Timeline: "Authorization: Bearer top-secret", Files: []EvidenceFile{{Name: "objects.yaml", Content: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: credentials
data:
  password: c2VjcmV0
stringData:
  token: plain
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  namespace: payments
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: example/app:1.2.3
`)}}}
	got, err := NormalizeIncident(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"top-secret", "c2VjcmV0", "plain"} {
		if strings.Contains(got.SanitizedEvidence, secret) {
			t.Fatalf("secret %q leaked", secret)
		}
	}
	if !strings.Contains(got.SanitizedEvidence, "kind=Deployment") {
		t.Fatalf("facts not extracted: %s", got.SanitizedEvidence)
	}
}

func TestStructuredIncidentRedactsSecretsRecursivelyAndPreservesContext(t *testing.T) {
	input := IncidentInput{Files: []EvidenceFile{
		{Name: "objects.yaml", Content: []byte(`apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: Secret
  metadata:
    name: checkout-credentials
    namespace: payments
    labels:
      app: checkout
  data:
    password: YWRtaW4=
  stringData:
    token: kube-token-value
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: checkout-config
    namespace: payments
  data:
    PORT: "8080"
    LATENCY_MS: "720"
    API_TOKEN: config-token-value
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  namespace: payments
spec:
  replicas: 3
`)},
		{Name: "nested.json", Content: []byte(`{"service":"checkout","nested":{"Password":"json-password-value","refresh-token":"refresh-token-value"},"request_rate":150}`)},
	}}
	got, err := NormalizeIncident(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"YWRtaW4=", "kube-token-value", "config-token-value", "json-password-value", "refresh-token-value"} {
		if strings.Contains(got.SanitizedEvidence, secret) {
			t.Fatalf("structured secret %q leaked: %s", secret, got.SanitizedEvidence)
		}
	}
	for _, safe := range []string{"checkout-credentials", "payments", "checkout-config", `"PORT":"8080"`, `"LATENCY_MS":"720"`, "request_rate", "replicas=3"} {
		if !strings.Contains(got.SanitizedEvidence, safe) {
			t.Fatalf("safe operational context %q was lost: %s", safe, got.SanitizedEvidence)
		}
	}
	if len(got.Files) != 0 {
		t.Fatal("raw files retained in normalized incident")
	}
}

func TestTextRedactionCoversCredentialsWithoutDestroyingOperationalContext(t *testing.T) {
	text := `HTTP 500 pod checkout-7d9c namespace payments replicas=3 latency_ms=720 CPU=92% request_rate=150
Authorization: Bearer sentinel-token-123
Authorization: Basic dXNlcjpwYXNz
password=SUPER_SECRET_DB_PASSWORD_9fd1
CLIENT_SECRET=client-secret-value
API_TOKEN=api-token-value
postgres://dbuser:supersecret@db.internal:5432/app
-----BEGIN PRIVATE KEY-----
private-key-material
-----END PRIVATE KEY-----
AKIAIOSFODNN7EXAMPLE
ghp_abcdefghijklmnopqrstuvwxyz123456
token bucket exhausted
secret-controller is healthy`
	got := RedactSecrets(text)
	for _, secret := range []string{"sentinel-token-123", "dXNlcjpwYXNz", "SUPER_SECRET_DB_PASSWORD_9fd1", "client-secret-value", "api-token-value", "supersecret", "private-key-material", "AKIAIOSFODNN7EXAMPLE", "ghp_abcdefghijklmnopqrstuvwxyz123456"} {
		if strings.Contains(got, secret) {
			t.Fatalf("plaintext secret %q leaked: %s", secret, got)
		}
	}
	for _, safe := range []string{"HTTP 500", "checkout-7d9c", "payments", "replicas=3", "latency_ms=720", "CPU=92%", "request_rate=150", "postgres://dbuser:[REDACTED]@db.internal:5432/app", "token bucket exhausted", "secret-controller"} {
		if !strings.Contains(got, safe) {
			t.Fatalf("safe context %q was lost: %s", safe, got)
		}
	}
}

func TestRedactionIsIdempotent(t *testing.T) {
	text := "Authorization: Bearer token-value\npassword=secret-value\npostgres://user:pass-value@db/app"
	once := RedactSecrets(text)
	if twice := RedactSecrets(once); twice != once {
		t.Fatalf("text redaction is not idempotent:\nonce=%s\ntwice=%s", once, twice)
	}
	var structured any
	if err := json.Unmarshal([]byte(`{"nested":{"password":"secret-value"},"PORT":8080}`), &structured); err != nil {
		t.Fatal(err)
	}
	sanitizer := NewSanitizer()
	context := newRedactionContext()
	collectSensitiveValues(structured, context)
	onceValue := sanitizer.sanitizeValue(structured, context)
	twiceValue := sanitizer.sanitizeValue(onceValue, context)
	onceJSON, _ := json.Marshal(onceValue)
	twiceJSON, _ := json.Marshal(twiceValue)
	if string(onceJSON) != string(twiceJSON) {
		t.Fatalf("structured redaction is not idempotent: %s != %s", onceJSON, twiceJSON)
	}
}

func TestNormalizeIncidentRejectsUnsupportedAndOversizedFiles(t *testing.T) {
	if _, err := NormalizeIncident(IncidentInput{Files: []EvidenceFile{{Name: "incident.pdf", Content: []byte("x")}}}); err == nil {
		t.Fatal("expected unsupported file error")
	}
	if _, err := NormalizeIncident(IncidentInput{Files: []EvidenceFile{{Name: "large.log", Content: make([]byte, MaxFileSize+1)}}}); err == nil {
		t.Fatal("expected size error")
	}
	if _, err := NormalizeIncident(IncidentInput{Timeline: strings.Repeat("x", MaxSanitizedEvidence+1)}); err == nil {
		t.Fatal("expected oversized pasted incident error")
	}
	sentinel := "SUPER_SECRET_DB_PASSWORD_9fd1"
	_, err := NormalizeIncident(IncidentInput{Files: []EvidenceFile{{Name: sentinel + ".yaml", Content: []byte(": invalid")}}})
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("structured parser error leaked a secret-bearing filename: %v", err)
	}
}
