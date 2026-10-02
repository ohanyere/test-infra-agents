package agentgateway

import (
	"errors"
	"testing"
	"time"
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

func TestValidateTypedImageActions(t *testing.T) {
	target := &ActionTarget{Kind: "Deployment", Namespace: "checkout", Name: "checkout"}
	for _, actionType := range []string{"set_live_workload_image", "set_desired_workload_image"} {
		valid := Response{ProtocolVersion: ProtocolV1, Actions: []ProposedAction{{Type: actionType, Target: target, Parameters: map[string]any{"image": "registry.local/checkout:v17"}}}}
		if err := ValidateResponse(valid, []string{actionType}); err != nil {
			t.Fatalf("valid %s: %v", actionType, err)
		}
		wrongKind := Response{ProtocolVersion: ProtocolV1, Actions: []ProposedAction{{Type: actionType, Target: &ActionTarget{Kind: "Pod", Namespace: "checkout", Name: "checkout"}, Parameters: map[string]any{"image": "registry.local/checkout:v17"}}}}
		if err := ValidateResponse(wrongKind, []string{actionType}); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("non-Deployment %s error = %v", actionType, err)
		}
	}
	unknown := Response{ProtocolVersion: ProtocolV1, Actions: []ProposedAction{{Type: "teleport_workload_image", Target: target, Parameters: map[string]any{"image": "registry.local/checkout:v17"}}}}
	if err := ValidateResponse(unknown, []string{"teleport_workload_image"}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("unknown action error = %v", err)
	}
	if err := ValidateRequest(Request{ProtocolVersion: ProtocolV1, RunID: "550e8400-e29b-41d4-a716-446655440003", AgentVersionID: "550e8400-e29b-41d4-a716-446655440004", Task: AgentTask{Message: "restore"}, Observation: Observation{Timestamp: time.Now()}, AvailableActions: []string{"set_live_workload_image", "teleport_workload_image"}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown available action error = %v", err)
	}
}
