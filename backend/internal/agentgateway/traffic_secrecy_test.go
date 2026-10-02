package agentgateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Bounded proof that direct-HTTP agent traffic carries no infrastructure
// secrets. The request/response schemas are fixed structs; this test trips
// if a field is added without updating the allowlist, and scans a maximal
// payload for credential-shaped material. (MCP grant tokens travel only on
// the MCP path under an expiring scoped grant and are out of scope here:
// direct-HTTP requests must not carry ToolAccess at all.)
func TestDirectAgentTrafficCarriesNoSecrets(t *testing.T) {
	request := Request{
		ProtocolVersion: ProtocolV1, SessionID: "session", RunID: "550e8400-e29b-41d4-a716-446655440003",
		AgentVersionID: "550e8400-e29b-41d4-a716-446655440004", Task: AgentTask{Message: "Recover checkout"},
		Observation:      Observation{Timestamp: time.Now(), Workloads: []WorkloadObservation{{Kind: "Deployment", Namespace: "run", Name: "checkout", Image: "evalplane-reference:v18", DesiredReplicas: 2, AvailableReplicas: 2}}, Pods: []PodObservation{{Name: "pod", Namespace: "run"}}, Metrics: map[string]any{"error_rate_pct": 100}, RecentEvents: []RecentEvent{{Timestamp: time.Now(), Type: "Warning"}}},
		AvailableActions: []string{"inspect", "set_desired_workload_image", "no_op"},
		VisibleContext:   json.RawMessage(`{"service":"checkout"}`), AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"run","name":"checkout"}]`),
	}
	response := Response{ProtocolVersion: ProtocolV1, ReasoningSummary: "remediate", Actions: []ProposedAction{{Type: "set_desired_workload_image", Target: &ActionTarget{Kind: "Deployment", Namespace: "run", Name: "checkout"}, Parameters: map[string]any{"image": "evalplane-reference:v17"}}}}

	rawRequest, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var requestKeys map[string]any
	if err := json.Unmarshal(rawRequest, &requestKeys); err != nil {
		t.Fatal(err)
	}
	for key := range requestKeys {
		switch key {
		case "protocol_version", "session_id", "run_id", "agent_version_id", "task", "observation", "available_actions", "visible_context", "allowed_resources", "tool_access":
		default:
			t.Fatalf("unreviewed request field %q", key)
		}
	}
	if _, ok := requestKeys["tool_access"]; ok {
		t.Fatal("direct-HTTP request must not carry tool access")
	}
	for _, raw := range []json.RawMessage{rawRequest, rawResponse} {
		lowered := strings.ToLower(string(raw))
		for _, marker := range []string{"kubeconfig", "bearer ", "bearer\"", "password", "passwd", "-----begin", "clusters:", "users:", "client-certificate", "token\":", "\"token\""} {
			if strings.Contains(lowered, marker) {
				t.Fatalf("credential marker %q in agent traffic", marker)
			}
		}
		if strings.Contains(lowered, "://") {
			t.Fatalf("URL with authority in agent traffic: %.120s", lowered)
		}
	}
}
