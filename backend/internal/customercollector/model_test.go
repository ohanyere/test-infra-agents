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

func TestActionClaimValidationRejectsMalformedAndUnboundedClaims(t *testing.T) {
	now := time.Now().UTC()
	valid := SubmitActionClaimInput{
		ContractVersion: ActionClaimContractVersion,
		AgentVersionID:  "11111111-1111-4111-8111-111111111111",
		ClaimID:         "22222222-2222-4222-8222-222222222222",
		IdempotencyKey:  "33333333-3333-4333-8333-333333333333",
		ActionType:      "set_workload_image",
		Resource:        ClaimedResource{Kind: "Deployment", Namespace: "checkout", Name: "api.checkout", UID: "e47ac8b1-5509-4e5c-a321-9dd3bc8ef012"},
		StartedAt:       now.Add(-time.Minute), CompletedAt: now.Add(-30 * time.Second),
		Outcome: "executed", ExternalTrace: "abcdef0123456789",
	}
	if _, err := validateActionClaim(valid); err != nil {
		t.Fatalf("valid claim rejected: %v", err)
	}
	tests := map[string]func(*SubmitActionClaimInput){
		"bad schema":                      func(in *SubmitActionClaimInput) { in.ContractVersion = "future" },
		"malformed resource name":         func(in *SubmitActionClaimInput) { in.Resource.Name = "../deployment" },
		"malformed namespace":             func(in *SubmitActionClaimInput) { in.Resource.Namespace = "Checkout" },
		"missing resource UID":            func(in *SubmitActionClaimInput) { in.Resource.UID = "" },
		"action kind mismatch":            func(in *SubmitActionClaimInput) { in.ActionType = "restart_pod" },
		"unsupported action":              func(in *SubmitActionClaimInput) { in.ActionType = "exec" },
		"invalid outcome":                 func(in *SubmitActionClaimInput) { in.Outcome = "verified" },
		"reversed timestamps":             func(in *SubmitActionClaimInput) { in.StartedAt, in.CompletedAt = in.CompletedAt, in.StartedAt },
		"trace may not carry credentials": func(in *SubmitActionClaimInput) { in.ExternalTrace = "https://agent.example/run?token=secret" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := validateActionClaim(input); err == nil {
				t.Fatal("invalid action claim accepted")
			}
		})
	}
	if err := validateActionClaimClock(valid, now); err != nil {
		t.Fatalf("valid claim clock rejected: %v", err)
	}
	stale := valid
	stale.StartedAt = now.Add(-25 * time.Hour)
	if err := validateActionClaimClock(stale, now); err == nil {
		t.Fatal("stale claim accepted")
	}
	future := valid
	future.CompletedAt = now.Add(time.Minute)
	if err := validateActionClaimClock(future, now); err == nil {
		t.Fatal("future claim accepted")
	}
}

func TestActionClaimWindowBoundariesAndUTCNormalization(t *testing.T) {
	baseline := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	final := baseline.Add(10 * time.Second)
	input := SubmitActionClaimInput{StartedAt: baseline, CompletedAt: final}
	if err := validateActionClaimWindow(input, &baseline, &final, final.Add(time.Second), 15); err != nil {
		t.Fatalf("inclusive baseline/final boundaries rejected: %v", err)
	}
	before := input
	before.StartedAt = baseline.Add(-time.Microsecond)
	if err := validateActionClaimWindow(before, &baseline, &final, final, 15); err == nil {
		t.Fatal("action starting before the baseline was accepted")
	}
	after := input
	after.CompletedAt = final.Add(time.Microsecond)
	if err := validateActionClaimWindow(after, &baseline, &final, final.Add(time.Minute), 15); err == nil {
		t.Fatal("action completing after the final observation was accepted")
	}
	if err := validateActionClaimWindow(input, nil, &final, final, 15); err == nil {
		t.Fatal("claim without an observed baseline was accepted")
	}

	zone := time.FixedZone("customer-agent", -7*60*60)
	local := SubmitActionClaimInput{
		ContractVersion: ActionClaimContractVersion,
		AgentVersionID:  "11111111-1111-4111-8111-111111111111",
		ClaimID:         "22222222-2222-4222-8222-222222222222",
		IdempotencyKey:  "33333333-3333-4333-8333-333333333333",
		ActionType:      "set_workload_image",
		Resource:        ClaimedResource{Kind: "Deployment", Namespace: "checkout", Name: "api", UID: "e47ac8b1-5509-4e5c-a321-9dd3bc8ef012"},
		Outcome:         "executed",
	}
	local.StartedAt = baseline.In(zone)
	local.CompletedAt = final.In(zone)
	normalized, err := validateActionClaim(local)
	if err != nil {
		t.Fatalf("valid offset timestamps rejected: %v", err)
	}
	if normalized.StartedAt.Location() != time.UTC || normalized.CompletedAt.Location() != time.UTC || !normalized.StartedAt.Equal(baseline) || !normalized.CompletedAt.Equal(final) {
		t.Fatalf("timestamps were not normalized without changing instants: start=%s complete=%s", normalized.StartedAt, normalized.CompletedAt)
	}
}
