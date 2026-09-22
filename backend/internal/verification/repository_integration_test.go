package verification

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

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
			_, err := NewRepository().Insert(ctx, conn, Result{
				WorkspaceID: workspaceID, RunID: tc.runID, AgentVersionID: agentID,
				ScenarioVersionID: scenarioID, EnvironmentInstanceID: tc.environmentID,
				CompositionPlanID: tc.planID, VerifierContractVersion: ContractVersion,
				SnapshotContractVersion: SnapshotVersion, Validity: ValidityInvalidPlatform,
				OverallStatus: StatusInconclusive, Reason: "test", SourceMetadata: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatalf("verification insert failed for UUID values: %v", err)
			}
			var persistedEnvironment, persistedPlan *string
			if err := conn.QueryRow(ctx, `SELECT environment_instance_id::text, composition_plan_id::text FROM verification_results WHERE run_id=$1`, tc.runID).Scan(&persistedEnvironment, &persistedPlan); err != nil {
				t.Fatal(err)
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
}
