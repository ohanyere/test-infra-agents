package scenarioauthoring

import (
	"testing"

	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
)

func TestAuthoringCatalogEmitsCanonicalObservables(t *testing.T) {
	got := CurrentCapabilities().Observations
	want := observablecatalog.CanonicalIDs()
	if len(got) != len(want) {
		t.Fatalf("authoring observables = %v, catalog = %v", got, want)
	}
	for index, name := range got {
		if name != want[index] {
			t.Fatalf("authoring observable %d = %q, want %q", index, name, want[index])
		}
	}
}
