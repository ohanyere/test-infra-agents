package qualification

import (
	"os"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/runtimecontract"
)

func TestReproducibilityRecordsInfrastructureCatalog(t *testing.T) {
	a := Evaluate(basePolicy(), "agent", facts(evaluationsuite.Counts{Scheduled: 3, Valid: 3, Passed: 3, PassRate: pct(100)}))
	if len(a.Reproducibility) != 1 {
		t.Fatalf("reproducibility=%v", a.Reproducibility)
	}
	if a.Reproducibility[0].InfrastructureCapabilityCatalogVersion != "infrastructure-capability-catalog:v1" {
		t.Fatalf("manifest=%#v", a.Reproducibility[0])
	}
	if a.Reproducibility[0].InfrastructureCapabilityCatalogVersion != runtimecontract.InfrastructureCapabilityCatalogVersion {
		t.Fatalf("manifest diverges from runtimecontract")
	}
}

func TestClaimImmutabilityMigration(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000015_agent_capability_claims.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, want := range []string{
		"agent_version_capability_claims",
		"catalog_version",
		"infrastructure-capability-catalog:v1",
		"PRIMARY KEY (agent_version_id, capability_id)",
		"BEFORE UPDATE OR DELETE",
		"immutable",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration must contain %q", want)
		}
	}
}

func TestClaimWorkspaceIsolationMigration(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000015_agent_capability_claims.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "REFERENCES agent_versions(id) ON DELETE CASCADE") {
		t.Fatal("claims must cascade from agent_versions")
	}
}
