package operational

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type resourceRoundTrip func(*http.Request) (*http.Response, error)

func (f resourceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func resourceClient(path, body string, status int, t *testing.T) *http.Client {
	return &http.Client{Transport: resourceRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != path {
			t.Errorf("path %q", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func TestAcquireScopedKubernetesResources(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct{ kind, path, body string }{
		{"kubernetes_service", "/api/v1/namespaces/checkout/services/checkout", `{"kind":"Service","metadata":{"name":"checkout","namespace":"checkout","uid":"s1","resourceVersion":"9"},"spec":{"type":"LoadBalancer"},"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.1"}]}}}`},
		{"kubernetes_ingress", "/apis/networking.k8s.io/v1/namespaces/checkout/ingresses/checkout", `{"kind":"Ingress","metadata":{"name":"checkout","namespace":"checkout","uid":"i1","resourceVersion":"9"},"spec":{"rules":[{"host":"checkout.example","http":{"paths":[{"backend":{"service":{"name":"checkout"}}}]}}]}}`},
		{"kubernetes_networkpolicy", "/apis/networking.k8s.io/v1/namespaces/checkout/networkpolicies/checkout", `{"kind":"NetworkPolicy","metadata":{"name":"checkout","namespace":"checkout","uid":"n1","resourceVersion":"9"},"spec":{"policyTypes":["Ingress"],"ingress":[{}]}}`},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			client := resourceClient(tc.path, tc.body, http.StatusOK, t)
			raw, payload, observed, err := acquireKubernetesObject(context.Background(), client, DirectSource{Kind: tc.kind, Namespace: "checkout", ResourceName: "checkout", EndpointURL: "https://kubernetes.example"}, "", now)
			if err != nil || string(raw) != tc.body || !observed.Equal(now) {
				t.Fatalf("acquire: %v", err)
			}
			data, _ := json.Marshal(payload)
			var value struct {
				ResourceState KubernetesResourceState `json:"resource_state"`
			}
			if json.Unmarshal(data, &value) != nil || !value.ResourceState.Present || value.ResourceState.ResourceVersion != "9" {
				t.Fatalf("state: %s", data)
			}
		})
	}
}

func TestKubernetesAbsentStateRequiresAPIStatus(t *testing.T) {
	client := resourceClient("/api/v1/namespaces/checkout/services/checkout", `{"kind":"Status","reason":"NotFound","code":404}`, http.StatusNotFound, t)
	_, payload, _, err := acquireKubernetesObject(context.Background(), client, DirectSource{Kind: "kubernetes_service", Namespace: "checkout", ResourceName: "checkout", EndpointURL: "https://kubernetes.example"}, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(payload)
	var value struct {
		ResourceState KubernetesResourceState `json:"resource_state"`
	}
	if json.Unmarshal(data, &value) != nil || value.ResourceState.Present {
		t.Fatalf("unexpected state: %s", data)
	}
	if kubernetesNotFound([]byte(`404 page not found`)) {
		t.Fatal("unstructured 404 accepted")
	}
}

func TestExposureAndDeletionKeepOutcomeBoundaries(t *testing.T) {
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]any{"resource_state": KubernetesResourceState{Kind: "Service", Namespace: "checkout", Name: "checkout", Present: true, ServiceType: "LoadBalancer"}})
	record := DirectObservation{ID: "source-observation", Kind: "kubernetes_service", Payload: payload, ObservedAt: now, AcquiredAt: now}
	in := Input{DirectObservations: []DirectObservation{record}, EvaluatedAt: now, Freshness: time.Minute}
	f := assessExposureConfiguration(in, Rule{Namespace: "checkout", ResourceName: "checkout"})
	if f.Status != Inconclusive || f.Code != "PUBLIC_EXPOSURE_CONFIGURATION_CANDIDATE" || len(f.Evidence) != 1 {
		t.Fatalf("configuration promoted to reachability: %+v", f)
	}
	in.Actions = []Action{{ID: "audit", Kind: "Service", Namespace: "checkout", Name: "checkout", Operation: "delete", OriginAuthenticated: true, OccurredAt: now.Add(-time.Second)}}
	if f := assessDirectDeletion(in)[0]; f.Status != Inconclusive {
		t.Fatalf("present object treated as deleted: %+v", f)
	}
	payload, _ = json.Marshal(map[string]any{"resource_state": KubernetesResourceState{Kind: "Service", Namespace: "checkout", Name: "checkout", Present: false}})
	in.DirectObservations[0].Payload = payload
	if f := assessDirectDeletion(in)[0]; f.Status != Pass || f.Code != "DELETION_OBSERVED" {
		t.Fatalf("absent result: %+v", f)
	}
	newer := record
	newer.ID = "newer-present"
	newer.AcquiredAt = now.Add(time.Second)
	newer.ObservedAt = newer.AcquiredAt
	in.EvaluatedAt = newer.AcquiredAt
	in.DirectObservations = append(in.DirectObservations, newer)
	if f := assessDirectDeletion(in)[0]; f.Status != Inconclusive || f.Code != "RESOURCE_STILL_PRESENT_AFTER_DELETE" {
		t.Fatalf("older absence overrode newer presence: %+v", f)
	}
	in.DirectObservations = in.DirectObservations[:1]
	in.EvaluatedAt = now
	in.Actions[0].OriginAuthenticated = false
	if f := assessDirectDeletion(in)[0]; f.Status != Inconclusive {
		t.Fatalf("forwarded action promoted: %+v", f)
	}
}
