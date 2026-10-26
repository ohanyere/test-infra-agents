package migrationtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrations(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is invalid")
	}
	name := config.ConnConfig.Database
	if !strings.HasSuffix(name, "_test") && !strings.HasPrefix(name, "realverax_stage10a_") {
		t.Fatalf("refusing migration test on database %q: use an explicitly disposable test database", name)
	}

	migrationsPath, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	migrator, err := migrate.New("file://"+filepath.ToSlash(migrationsPath), databaseURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer migrator.Close()
	defer func() {
		if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Errorf("restore latest migration version: %v", err)
		}
	}()

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate up: %v", err)
	}
	assertVersion(t, migrator, 40)
	assertCoreTables(t, databaseURL)
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("migrate down 40: %v", err)
	}
	assertVersion(t, migrator, 39)
	assertStage10HSourceKinds(t, databaseURL, true)
	assertStage10H1Reachability(t, databaseURL, true)
	if stage10H1HasProbeData(t, databaseURL) {
		t.Log("migration 39 rollback not attempted: retained reachability data is incompatible with the older constraint")
		return
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("migrate down 39: %v", err)
	}
	assertVersion(t, migrator, 38)
	assertStage10H1Reachability(t, databaseURL, false)
	if stage10HHasNewKindData(t, databaseURL) {
		t.Log("migration 38 rollback not attempted: immutable new-kind source data would make the older constraint incompatible")
		if err := migrator.Up(); err != nil {
			t.Fatalf("restore 39: %v", err)
		}
		assertVersion(t, migrator, 40)
		return
	}

	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("migrate down one version: %v", err)
	}
	assertVersion(t, migrator, 37)
	assertStage10HSourceKinds(t, databaseURL, false)
	if stage10GHasIncompatibleData(t, databaseURL) {
		t.Log("migration 37 rollback not attempted: retained lesson decisions or guidance links are incompatible")
	} else {
		if err := migrator.Steps(-1); err != nil {
			t.Fatalf("migrate down 37: %v", err)
		}
		assertVersion(t, migrator, 36)
		assertStage10GAbsent(t, databaseURL)
	}
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate back up: %v", err)
	}
	assertVersion(t, migrator, 40)
	assertCoreTables(t, databaseURL)
	assertStage10HSourceKinds(t, databaseURL, true)
	assertStage10H1Reachability(t, databaseURL, true)
}

func assertStage10H1Reachability(t *testing.T, databaseURL string, enabled bool) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"customer_outcome_sources", "customer_direct_outcome_observations"} {
		var definition string
		if err := pool.QueryRow(context.Background(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, table, table+"_source_kind_check").Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(definition, "external_reachability") != enabled {
			t.Fatalf("migration 39 %s reachability enabled=%v: %s", table, enabled, definition)
		}
	}
}

func stage10H1HasProbeData(t *testing.T, databaseURL string) bool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM customer_outcome_sources WHERE source_kind='external_reachability') OR EXISTS(SELECT 1 FROM customer_direct_outcome_observations WHERE source_kind='external_reachability')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func stage10GHasIncompatibleData(t *testing.T, databaseURL string) bool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM operational_lesson_decisions WHERE decision IN ('rejected','retired')) OR EXISTS(SELECT 1 FROM operational_guidance_operation_links)`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func assertStage10HSourceKinds(t *testing.T, databaseURL string, enabled bool) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"customer_outcome_sources", "customer_direct_outcome_observations"} {
		var definition string
		err = pool.QueryRow(context.Background(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, table, table+"_source_kind_check").Scan(&definition)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(definition, "kubernetes_service") != enabled {
			t.Fatalf("migration 38 %s source kinds enabled=%v: %s", table, enabled, definition)
		}
	}
}

func stage10HHasNewKindData(t *testing.T, databaseURL string) bool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	err = pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM customer_outcome_sources WHERE source_kind IN ('kubernetes_service','kubernetes_ingress','kubernetes_networkpolicy')) OR EXISTS(SELECT 1 FROM customer_direct_outcome_observations WHERE source_kind IN ('kubernetes_service','kubernetes_ingress','kubernetes_networkpolicy'))`).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func assertStage10GAbsent(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.operational_guidance_operation_links') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("migration 37 rollback: guidance links exist=%v error=%v", exists, err)
	}
}

func assertStage10FAbsent(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.assurance_monitoring_plans') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("migration 36 rollback: monitoring plan exists=%v error=%v", exists, err)
	}
}

func assertStage10EAbsent(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.customer_direct_outcome_observations') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("migration 35 rollback: direct outcome table exists=%v, error=%v", exists, err)
	}
}

func assertStage10DAbsent(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.customer_authenticated_audit_events') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("migration 34 rollback: audit events table exists=%v, error=%v", exists, err)
	}
}

func assertStage9Absent(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect for rollback inspection: %v", err)
	}
	defer pool.Close()
	var episodeExists, scopeKeyExists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.operational_episodes') IS NOT NULL, EXISTS (SELECT 1 FROM pg_constraint WHERE conname='operational_assessment_reports_episode_scope_key')`).Scan(&episodeExists, &scopeKeyExists); err != nil || episodeExists || scopeKeyExists {
		t.Fatalf("migration 33 rollback: episodes=%v scope_key=%v error=%v", episodeExists, scopeKeyExists, err)
	}
}

func assertVersion(t *testing.T, migrator *migrate.Migrate, want uint) {
	t.Helper()
	got, dirty, err := migrator.Version()
	if err != nil || dirty || got != want {
		t.Fatalf("migration version = (%d, dirty=%v, err=%v), want (%d, false, nil)", got, dirty, err, want)
	}
}

func assertCoreTables(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect for schema inspection: %v", err)
	}
	defer pool.Close()

	tables := []string{"organizations", "workspaces", "users", "workspace_members", "agents", "agent_versions", "agent_version_connections", "agent_version_capability_claims", "mcp_run_grants", "scenarios", "scenario_versions", "scenario_criteria", "task_families", "scenario_version_task_family", "scenario_version_reference_remediations", "scenario_certifications", "evaluation_batches", "evaluation_batch_agents", "runs", "run_events", "agent_actions", "evaluations", "evaluation_criterion_results", "evaluation_findings", "verification_results", "password_credentials", "sessions", "evaluation_suites", "evaluation_suite_versions", "evaluation_suite_version_scenarios", "suite_executions", "suite_execution_agents", "suite_execution_batches", "suite_execution_run_variants", "agent_regression_comparisons", "qualification_policies", "qualification_policy_versions", "qualification_assessments", "qualification_rule_results", "scenario_version_environment_requirements", "environment_instances", "world_specs", "composition_plans", "environment_component_instances", "customer_kubernetes_environments", "customer_kubernetes_collectors", "customer_observation_sessions", "customer_observation_batches", "customer_agent_action_claims", "customer_action_evidence", "runner_audit_test_registrations", "runner_audit_test_sessions", "runner_kubernetes_audit_evidence", "customer_audit_source_registrations", "customer_kubernetes_audit_records", "customer_audit_reconciliation_cases", "customer_audit_reconciliation_events", "operational_contract_versions", "customer_service_metric_samples", "operational_assessment_reports", "operational_approval_records", "operational_finding_history", "assurance_notification_events", "assurance_notification_deliveries", "assurance_evaluation_jobs", "operational_episodes", "operational_episode_relationships", "operational_episode_decisions", "operational_lessons", "operational_lesson_decisions", "operational_guidance_credentials", "operational_guidance_requests", "operational_guidance_experiments", "qualification_plans", "qualification_plan_suites", "capability_coverage_assessments", "billing_accounts", "billing_usage_ledger", "billing_provider_events"}
	tables = append(tables, "customer_audit_mtls_sources", "customer_authenticated_audit_events", "customer_execution_issuers", "customer_execution_receipts", "customer_attribution_decisions", "customer_outcome_sources", "customer_direct_outcome_observations", "operational_approval_revocations", "customer_gateway_preflight_decisions", "assurance_monitoring_plans", "assurance_monitoring_plan_sources", "assurance_monitoring_events", "operational_guidance_operation_links")
	for _, table := range tables {
		var exists bool
		query := `SELECT to_regclass($1) IS NOT NULL`
		if err := pool.QueryRow(context.Background(), query, fmt.Sprintf("public.%s", table)).Scan(&exists); err != nil || !exists {
			t.Fatalf("table %s exists = %v, error = %v", table, exists, err)
		}
	}

	objects := []string{
		"billing_usage_ledger_workspace_id_idempotency_key_key",
		"billing_provider_events_provider_provider_event_id_key",
		"qualification_plans_immutable",
		"verification_results_immutable",
		"runs_immutable_provenance",
		"customer_action_evidence_immutable",
		"customer_action_evidence_no_truncate",
		"evaluation_batches_ci_idempotency_idx",
	}
	for _, object := range objects {
		var exists bool
		query := `SELECT to_regclass($1) IS NOT NULL`
		if object == "qualification_plans_immutable" || object == "verification_results_immutable" || object == "runs_immutable_provenance" || object == "customer_action_evidence_immutable" || object == "customer_action_evidence_no_truncate" {
			query = `SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = $1 AND NOT tgisinternal)`
		}
		if err := pool.QueryRow(context.Background(), query, object).Scan(&exists); err != nil || !exists {
			t.Fatalf("schema object %s exists = %v, error = %v", object, exists, err)
		}
	}
	for table, column := range map[string]string{"agent_versions": "implementation_identity", "runs": "agent_implementation_identity", "verification_results": "provenance"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, table, column).Scan(&exists); err != nil || !exists {
			t.Fatalf("column %s.%s exists = %v, error = %v", table, column, exists, err)
		}
	}
	for table, columns := range map[string][]string{
		"operational_episodes":              {"payload_bytes", "content_digest", "eligibility", "report_revision"},
		"operational_guidance_requests":     {"response_bytes", "response_digest", "credential_id"},
		"operational_episode_relationships": {"observed", "provenance", "source_reference"},
	} {
		for _, column := range columns {
			var exists bool
			if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, table, column).Scan(&exists); err != nil || !exists {
				t.Fatalf("column %s.%s exists = %v, error = %v", table, column, exists, err)
			}
		}
	}
	for _, constraint := range []string{"operational_assessment_reports_episode_scope_key", "operational_episode_report_scope_fkey"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname=$1)`, constraint).Scan(&exists); err != nil || !exists {
			t.Fatalf("constraint %s exists = %v, error = %v", constraint, exists, err)
		}
	}
	var reportScopeDefinition string
	if err := pool.QueryRow(context.Background(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='operational_episode_report_scope_fkey'`).Scan(&reportScopeDefinition); err != nil || !strings.Contains(reportScopeDefinition, "environment_id") || !strings.Contains(reportScopeDefinition, "session_id") {
		t.Fatalf("episode report FK does not bind environment and session: %q, error=%v", reportScopeDefinition, err)
	}
	var coverageScopeUnique bool
	if err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'capability_coverage_assessments'::regclass AND contype = 'u'
		)`).Scan(&coverageScopeUnique); err != nil || !coverageScopeUnique {
		t.Fatalf("capability coverage scope unique constraint exists = %v, error = %v", coverageScopeUnique, err)
	}
}
