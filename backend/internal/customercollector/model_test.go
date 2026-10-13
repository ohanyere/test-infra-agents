package customercollector

import (
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

func TestEnvironmentScopeValidation(t *testing.T) {
	valid := CreateEnvironmentInput{Name: "prod", ClusterIdentifier: "cluster-a", Namespaces: []string{"apps", "payments"}, Tier: "production"}
	if _, err := normalizeEnvironment(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*CreateEnvironmentInput){
		"invalid namespace":   func(in *CreateEnvironmentInput) { in.Namespaces = []string{"../default"} },
		"duplicate namespace": func(in *CreateEnvironmentInput) { in.Namespaces = []string{"apps", "apps"} },
		"invalid tier":        func(in *CreateEnvironmentInput) { in.Tier = "development" },
		"empty scope":         func(in *CreateEnvironmentInput) { in.Namespaces = nil },
	} {
		t.Run(name, func(t *testing.T) {
			in := valid
			mutate(&in)
			if _, err := normalizeEnvironment(in); err == nil {
				t.Fatal("invalid environment accepted")
			}
		})
	}
}

func TestCollectorCredentialIsOneWayAndRotatable(t *testing.T) {
	first, firstHash, err := newCredential()
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := newCredential()
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(first) || !validToken(second) || first == second || string(firstHash) == string(secondHash) {
		t.Fatal("credentials were not independently generated")
	}
	if string(tokenDigest(first)) != string(firstHash) {
		t.Fatal("stored credential digest does not match token")
	}
}

func TestEvidenceBatchValidationAndScope(t *testing.T) {
	now := time.Now().UTC()
	input := EvidenceBatch{BatchID: "11111111-1111-4111-8111-111111111111", Sequence: 1, SchemaVersion: ContractVersion, Phase: "baseline", CapturedAt: now, Observation: agentgateway.Observation{Timestamp: now, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "apps", DesiredReplicas: 2}}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}}
	if err := validateBatch(input, now); err != nil {
		t.Fatal(err)
	}
	if err := resourceScope([]string{"Deployment", "Pod"}, []string{"apps"}, input); err != nil {
		t.Fatal(err)
	}
	if err := resourceScope([]string{"Deployment", "Pod"}, []string{"payments"}, input); err == nil {
		t.Fatal("out-of-scope namespace accepted")
	}
	input.SchemaVersion = "future"
	if err := validateBatch(input, now); err == nil {
		t.Fatal("unsupported evidence schema accepted")
	}
	input.SchemaVersion = ContractVersion
	input.Observation.Metrics = map[string]any{"availability_pct": 99.9}
	if err := validateBatch(input, now); err == nil {
		t.Fatal("unsupported telemetry accepted")
	}
	input.Observation.Metrics = map[string]any{}
	input.CapturedAt = now.Add(-6 * time.Minute)
	if err := validateBatch(input, now); err == nil {
		t.Fatal("stale observation accepted")
	}
}

func TestEvidencePhasesAreExplicit(t *testing.T) {
	now := time.Now().UTC()
	base := EvidenceBatch{BatchID: "22222222-2222-4222-8222-222222222222", Sequence: 1, SchemaVersion: ContractVersion, CapturedAt: now, Phase: "baseline", Observation: agentgateway.Observation{Timestamp: now}}
	if err := validateBatch(base, now); err != nil {
		t.Fatal(err)
	}
	base.Phase = "pretend-final"
	if err := validateBatch(base, now); err == nil {
		t.Fatal("unsupported phase accepted")
	}
}

func TestEvidenceOrderingRejectsReplayAndInsufficientWindow(t *testing.T) {
	now := time.Now().UTC()
	baseline := EvidenceBatch{Sequence: 1, Phase: "baseline", CapturedAt: now}
	if err := validateOrder(1, 0, "", nil, baseline); err != nil {
		t.Fatal(err)
	}
	if err := validateOrder(2, 1, "baseline", &now, baseline); err == nil {
		t.Fatal("replayed baseline accepted")
	}
	final := EvidenceBatch{Sequence: 2, Phase: "final", CapturedAt: now.Add(time.Second)}
	if err := validateOrder(2, 1, "baseline", &now, final); err == nil {
		t.Fatal("final accepted without stability samples")
	}
	stability := EvidenceBatch{Sequence: 2, Phase: "stability", CapturedAt: now.Add(time.Second)}
	if err := validateOrder(2, 1, "baseline", &now, stability); err != nil {
		t.Fatal(err)
	}
	if err := validateOrder(4, 3, "stability", nil, EvidenceBatch{Sequence: 4, Phase: "final", CapturedAt: now.Add(4 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := validateOrder(5, 4, "final", nil, EvidenceBatch{Sequence: 5, Phase: "stability", CapturedAt: now.Add(5 * time.Second)}); err == nil {
		t.Fatal("evidence accepted after final")
	}
}

func TestObservationWindowUsesServerReceiptTimes(t *testing.T) {
	baselineReceipt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if err := validateObservationWindow(baselineReceipt.Add(59*time.Second), baselineReceipt, 60); err == nil {
		t.Fatal("client timestamps must not bypass server-received observation duration")
	}
	if err := validateObservationWindow(baselineReceipt.Add(60*time.Second), baselineReceipt, 60); err != nil {
		t.Fatalf("complete server-received observation duration rejected: %v", err)
	}
}
