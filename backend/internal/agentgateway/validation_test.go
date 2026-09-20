package agentgateway

import (
	"errors"
	"testing"
)

func TestValidateSetWorkloadImage(t *testing.T) {
	target := &ActionTarget{Kind: "Deployment", Namespace: "checkout", Name: "checkout"}
	valid := Response{ProtocolVersion: ProtocolV1, Actions: []ProposedAction{{Type: "set_workload_image", Target: target, Parameters: map[string]any{"image": "registry.local/checkout:v17"}}}}
	if err := ValidateResponse(valid, []string{"set_workload_image"}); err != nil {
		t.Fatalf("valid image action: %v", err)
	}

	unsafe := valid
	unsafe.Actions = []ProposedAction{{Type: "set_workload_image", Target: target, Parameters: map[string]any{"image": "https://token@example.invalid/checkout:v17"}}}
	if err := ValidateResponse(unsafe, []string{"set_workload_image"}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("unsafe image action error = %v", err)
	}
}
