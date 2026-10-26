package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCollectorSendsScopedReadOnlySnapshot(t *testing.T) {
	var calls int
	token := "rvxc_" + strings.Repeat("a", 43)
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token {
			return nil, fmt.Errorf("request method/auth=%s/%q", r.Method, r.Header.Get("Authorization"))
		}
		var body struct {
			Sequence    int64  `json:"sequence"`
			Phase       string `json:"phase"`
			Observation struct {
				Workloads []struct{ Name, Namespace string } `json:"workloads"`
				Pods      []struct{ Ready bool }             `json:"pods"`
				Metrics   map[string]any                     `json:"metrics"`
			} `json:"observation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		if body.Sequence != 1 || body.Phase != "baseline" || len(body.Observation.Workloads) != 1 || body.Observation.Workloads[0].Namespace != "apps" || len(body.Observation.Pods) != 1 || body.Observation.Pods[0].Ready || len(body.Observation.Metrics) != 0 {
			return nil, fmt.Errorf("unexpected collector payload: %+v", body)
		}
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	replicas := int32(2)
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: "apps", UID: "uid", ResourceVersion: "7"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}}, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "checkout-pod", Namespace: "apps"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
	c := &collector{client: client, http: &http.Client{Transport: transport}, apiURL: "http://127.0.0.1:1234", sessionID: "11111111-1111-4111-8111-111111111111", credential: token, namespaces: []string{"apps"}}
	if err := c.send("baseline"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("API calls=%d", calls)
	}
}

func TestCollectorConfigRequiresSecureAPIAndEnoughEvidenceWindow(t *testing.T) {
	token := "rvxc_" + strings.Repeat("a", 43)
	if err := validateConfig("http://example.com", "session", token, []string{"apps"}, 10*time.Second, 30*time.Second); err == nil {
		t.Fatal("non-TLS external API accepted")
	}
	if err := validateConfig("https://api.example.com", "session", token, []string{"apps"}, 10*time.Second, 20*time.Second); err == nil {
		t.Fatal("window incapable of supplying two stability samples accepted")
	}
}
