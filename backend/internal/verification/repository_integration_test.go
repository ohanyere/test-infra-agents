package verification

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/provenance"
	"github.com/jackc/pgx/v5"
)

// A temporary table exercises the repository's real INSERT expression against
// PostgreSQL UUID columns without changing existing scratch database rows.
func TestInsertVerificationUUIDColumns(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(ctx, `CREATE TEMP TABLE verification_results (
		id text DEFAULT 'test-result', created_at timestamptz DEFAULT now(),
		workspace_id uuid, run_id uuid UNIQUE, agent_version_id uuid, scenario_version_id uuid,
		environment_instance_id uuid, composition_plan_id uuid, composition_plan_hash text,
		verifier_contract_version text, snapshot_contract_version text,
		baseline_snapshot jsonb, final_snapshot jsonb, intent_result jsonb, action_inspection jsonb,
		infrastructure_diff jsonb, outcome_result jsonb, invariant_results jsonb,
		side_effect_result jsonb, stability_result jsonb, forward_risk_findings jsonb,
		evidence_assessment jsonb, provenance jsonb,
		platform_validity text, overall_status text, reason text, source_metadata jsonb,
		evidence_started_at timestamptz, evidence_completed_at timestamptz
	)`)
	if err != nil {
		t.Fatal(err)
	}
	const workspaceID = "11111111-1111-4111-8111-111111111111"
	const agentID = "22222222-2222-4222-8222-222222222222"
	const scenarioID = "33333333-3333-4333-8333-333333333333"
	const environmentID = "44444444-4444-4444-8444-444444444444"
	const planID = "55555555-5555-4555-8555-555555555555"
	for _, tc := range []struct {
		name, runID, environmentID, planID string
	}{
		{"present", "66666666-6666-4666-8666-666666666666", environmentID, planID},
		{"absent", "77777777-7777-4777-8777-777777777777", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinned := &provenance.Verification{ContractVersion: provenance.ContractVersion, AgentImplementation: provenance.AgentImplementationIdentity{ContractVersion: provenance.AgentIdentityContractV1, Kind: provenance.KindSourceRevision, Identifier: "revision-a", ProvenanceLevel: provenance.LevelDeclared}, Contracts: provenance.ContractIdentities{Verifier: ContractVersion}, Providers: []provenance.ProviderIdentity{{Role: "workload_state", ProviderID: "kubernetes-api"}}}
			_, err := NewRepository().Insert(ctx, conn, Result{
				WorkspaceID: workspaceID, RunID: tc.runID, AgentVersionID: agentID,
				ScenarioVersionID: scenarioID, EnvironmentInstanceID: tc.environmentID,
				CompositionPlanID: tc.planID, VerifierContractVersion: ContractVersion,
				SnapshotContractVersion: SnapshotVersion, Validity: ValidityInvalidPlatform,
				OverallStatus: StatusInconclusive, Reason: "test", SourceMetadata: json.RawMessage(`{}`),
				Provenance:         pinned,
				ActionInspection:   ActionInspection{Attribution: AttributionAssessment{ContractVersion: AttributionContractVersion, Required: true, Status: AttributionAttributed, Reason: "test", Actions: []ActionAttribution{{ActionID: "action-1", Status: AttributionAttributed, Resource: "Deployment/run/checkout", RequestedValue: "checkout:v17"}}}},
				EvidenceAssessment: EvidenceAssessment{ContractVersion: evidencecontract.ContractV1, Requirements: evidencecontract.DefaultRequirements(), Dimensions: []EvidenceDimensionAssessment{{EvidenceType: "workload_state", Applicability: applicable, RequirementStatus: string(evidencecontract.Required), Availability: available, SourceProvider: "kubernetes-api", Reason: "test", EvidenceReferences: []string{"run_event:1"}}}},
			})
			if err != nil {
				t.Fatalf("verification insert failed for UUID values: %v", err)
			}
			var persistedEnvironment, persistedPlan *string
			var assessmentJSON, attributionJSON, provenanceJSON []byte
			if err := conn.QueryRow(ctx, `SELECT environment_instance_id::text, composition_plan_id::text, evidence_assessment, action_inspection, provenance FROM verification_results WHERE run_id=$1`, tc.runID).Scan(&persistedEnvironment, &persistedPlan, &assessmentJSON, &attributionJSON, &provenanceJSON); err != nil {
				t.Fatal(err)
			}
			var persistedProvenance provenance.Verification
			if err := json.Unmarshal(provenanceJSON, &persistedProvenance); err != nil || persistedProvenance.AgentImplementation.Identifier != "revision-a" || len(persistedProvenance.Providers) != 1 {
				t.Fatalf("provenance did not round trip: provenance=%+v err=%v", persistedProvenance, err)
			}
			var assessment EvidenceAssessment
			if err := json.Unmarshal(assessmentJSON, &assessment); err != nil || assessment.ContractVersion != evidencecontract.ContractV1 || len(assessment.Dimensions) != 1 {
				t.Fatalf("evidence assessment did not round trip: assessment=%+v err=%v", assessment, err)
			}
			var inspection ActionInspection
			if err := json.Unmarshal(attributionJSON, &inspection); err != nil || inspection.Attribution.ContractVersion != AttributionContractVersion || inspection.Attribution.Status != AttributionAttributed || len(inspection.Attribution.Actions) != 1 {
				t.Fatalf("action attribution did not round trip: inspection=%+v err=%v", inspection, err)
			}
			if tc.environmentID == "" {
				if persistedEnvironment != nil || persistedPlan != nil {
					t.Fatalf("absent UUIDs must persist as NULL: environment=%v plan=%v", persistedEnvironment, persistedPlan)
				}
			} else if persistedEnvironment == nil || persistedPlan == nil || *persistedEnvironment != tc.environmentID || *persistedPlan != tc.planID {
				t.Fatalf("UUIDs changed: environment=%v plan=%v", persistedEnvironment, persistedPlan)
			}
		})
	}
	const historicalRunID = "77777777-7777-4777-8777-777777777777"
	if _, err := conn.Exec(ctx, `UPDATE verification_results SET provenance=NULL WHERE run_id=$1`, historicalRunID); err != nil {
		t.Fatal(err)
	}
	historical, err := NewRepository().Get(ctx, conn, workspaceID, historicalRunID)
	if err != nil || historical.Provenance != nil {
		t.Fatalf("historical pre-Stage-4 row must remain readable: provenance=%+v err=%v", historical.Provenance, err)
	}
}
