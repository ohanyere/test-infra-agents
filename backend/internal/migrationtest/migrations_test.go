package migrationtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	assertVersion(t, migrator, 27)
	assertCoreTables(t, databaseURL)

	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("migrate down one version: %v", err)
	}
	assertVersion(t, migrator, 26)
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate back up: %v", err)
	}
	assertVersion(t, migrator, 27)
	assertCoreTables(t, databaseURL)
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

	tables := []string{"organizations", "workspaces", "users", "workspace_members", "agents", "agent_versions", "agent_version_connections", "agent_version_capability_claims", "mcp_run_grants", "scenarios", "scenario_versions", "scenario_criteria", "task_families", "scenario_version_task_family", "scenario_version_reference_remediations", "scenario_certifications", "evaluation_batches", "evaluation_batch_agents", "runs", "run_events", "agent_actions", "evaluations", "evaluation_criterion_results", "evaluation_findings", "verification_results", "password_credentials", "sessions", "evaluation_suites", "evaluation_suite_versions", "evaluation_suite_version_scenarios", "suite_executions", "suite_execution_agents", "suite_execution_batches", "suite_execution_run_variants", "agent_regression_comparisons", "qualification_policies", "qualification_policy_versions", "qualification_assessments", "qualification_rule_results", "scenario_version_environment_requirements", "environment_instances", "world_specs", "composition_plans", "environment_component_instances", "customer_kubernetes_environments", "customer_kubernetes_collectors", "customer_observation_sessions", "customer_observation_batches", "customer_agent_action_claims", "qualification_plans", "qualification_plan_suites", "capability_coverage_assessments", "billing_accounts", "billing_usage_ledger", "billing_provider_events"}
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
		"evaluation_batches_ci_idempotency_idx",
	}
	for _, object := range objects {
		var exists bool
		query := `SELECT to_regclass($1) IS NOT NULL`
		if object == "qualification_plans_immutable" || object == "verification_results_immutable" || object == "runs_immutable_provenance" {
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
	var coverageScopeUnique bool
	if err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'capability_coverage_assessments'::regclass AND contype = 'u'
		)`).Scan(&coverageScopeUnique); err != nil || !coverageScopeUnique {
		t.Fatalf("capability coverage scope unique constraint exists = %v, error = %v", coverageScopeUnique, err)
	}
}
