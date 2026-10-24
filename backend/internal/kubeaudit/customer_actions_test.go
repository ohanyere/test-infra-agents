package kubeaudit

import "testing"

func TestCustomerActionTargetNarrowCoverage(t *testing.T) {
	base := Event{APIVersion: AuditAPIVersion, Kind: "Event", AuditID: "audit-id", Stage: "ResponseComplete", Verb: "delete", Response: Response{Code: 200}}
	tests := []struct{ group, resource, namespace, name, kind, wantNamespace string }{
		{"apps", "deployments", "checkout", "checkout", "Deployment", "checkout"},
		{"", "services", "checkout", "public", "Service", "checkout"},
		{"networking.k8s.io", "ingresses", "checkout", "public", "Ingress", "checkout"},
		{"networking.k8s.io", "networkpolicies", "checkout", "restricted", "NetworkPolicy", "checkout"},
		{"", "namespaces", "", "checkout", "Namespace", "checkout"},
	}
	for _, tt := range tests {
		event := base
		event.ObjectRef = ObjectRef{APIGroup: tt.group, APIVersion: "v1", Resource: tt.resource, Namespace: tt.namespace, Name: tt.name}
		kind, namespace, ok := CustomerActionTarget(event)
		if !ok || kind != tt.kind || namespace != tt.wantNamespace {
			t.Fatalf("target %s: %s/%s %t", tt.resource, kind, namespace, ok)
		}
		event.Response.Code = 403
		if _, _, ok = CustomerActionTarget(event); ok {
			t.Fatalf("failed %s response was treated as observed mutation", tt.resource)
		}
		event.Response.Code = 201
		event.Verb = "create"
		if _, _, ok = CustomerActionTarget(event); !ok {
			t.Fatalf("named %s creation was missed", tt.resource)
		}
		event.ObjectRef.Name = ""
		if _, _, ok = CustomerActionTarget(event); ok {
			t.Fatalf("unnamed %s creation was accepted", tt.resource)
		}
	}
	unsupported := base
	unsupported.ObjectRef = ObjectRef{APIGroup: "", APIVersion: "v1", Resource: "secrets", Namespace: "checkout", Name: "secret"}
	if _, _, ok := CustomerActionTarget(unsupported); ok {
		t.Fatal("unsupported Secret operation was accepted")
	}
}
