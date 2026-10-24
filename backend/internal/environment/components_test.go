package environment

import (
	"encoding/json"
	"testing"
)

func TestComponentInstanceValidationRejectsSecretRefs(t *testing.T) {
	base := EnvironmentComponentInstance{WorkspaceID: "w", EnvironmentInstanceID: "e", ComponentID: "cluster", ComponentType: "kubernetes", AdapterID: "kind", AdapterContractVersion: "adapter:v1", ProviderID: "kind-dev:v1", ReproducibilityMetadata: json.RawMessage(`{}`)}
	for _, ref := range []string{"k8s://ns?token=secret", "Bearer token", "kubeconfig=/tmp/config", "password=p"} {
		base.ProviderRef = ref
		if validateComponentInstance(base) == nil {
			t.Fatalf("unsafe ref accepted: %s", ref)
		}
	}
	base.ProviderRef = "k8s://namespace/eval-run"
	if err := validateComponentInstance(base); err != nil {
		t.Fatal(err)
	}
}
