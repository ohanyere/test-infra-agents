package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/srequal"
)

func qualificationTasksRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger,
		WithIdentityService(fakeIdentity{}),
		WithAuthentication(fakeAuth{}, fakeAuth{}, false, 0, ""))
}

func TestQualificationTasksList(t *testing.T) {
	w := httptest.NewRecorder()
	qualificationTasksRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qualification-tasks", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body listResponse[srequal.SafeView]
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Kubernetes/SRE runnable tasks plus the GitOps/progressive-delivery
	// catalog, which is structurally served but truthfully not runnable
	// until a real GitOps provider exists.
	if len(body.Items) < 20 || len(body.Items) > 30 {
		t.Fatalf("expected SRE + GitOps tasks, got %d", len(body.Items))
	}
	runnable, unavailable := 0, 0
	for _, item := range body.Items {
		if item.TaskFamily == "" || item.CaseType == "" || len(item.Capabilities) == 0 {
			t.Fatalf("task %q missing safe metadata: %+v", item.ID, item)
		}
		if item.Runnable {
			runnable++
			if item.EligibleProfile != "kubernetes.namespace:v1" {
				t.Fatalf("task %q must be runnable on the namespace profile: %+v", item.ID, item)
			}
		} else {
			unavailable++
			if item.EligibleProfile != "" {
				t.Fatalf("unavailable task %q must not name an eligible profile: %+v", item.ID, item)
			}
			if item.CertificationNote == "" || item.SchedulerReason == "" {
				t.Fatalf("unavailable task %q must explain availability: %+v", item.ID, item)
			}
		}
	}
	if runnable < 10 || runnable > 14 {
		t.Fatalf("expected 10-14 runnable SRE tasks, got %d", runnable)
	}
	if unavailable < 10 || unavailable > 14 {
		t.Fatalf("expected 10-14 unavailable GitOps/delivery tasks, got %d", unavailable)
	}
	raw := strings.ToLower(w.Body.String())
	for _, marker := range []string{"ground_truth", "reference_remediation", "referenceactions", "hidden_variant", "evaluator-only-truth"} {
		if strings.Contains(raw, marker) {
			t.Fatalf("catalog response leaks evaluator-only marker %q", marker)
		}
	}
}

func TestQualificationTasksListContainsGitOps(t *testing.T) {
	w := httptest.NewRecorder()
	qualificationTasksRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qualification-tasks", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body listResponse[srequal.SafeView]
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byID := map[string]srequal.SafeView{}
	for _, item := range body.Items {
		byID[item.ID] = item
	}
	for _, id := range []string{"gitops-live-deployment-drift", "gitops-bad-desired-state", "delivery-canary-error-regression", "delivery-healthy-canary-hold"} {
		view, ok := byID[id]
		if !ok {
			t.Fatalf("task %q missing from catalog API", id)
		}
		if view.Runnable {
			t.Fatalf("task %q must not be runnable without a real provider", id)
		}
		if len(view.RequiredComponents) == 0 || view.CertificationNote == "" {
			t.Fatalf("task %q missing extended safe metadata: %+v", id, view)
		}
	}
}

func TestQualificationTaskGet(t *testing.T) {
	w := httptest.NewRecorder()
	qualificationTasksRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qualification-tasks/traffic-capacity-saturation", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var view srequal.SafeView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.ID != "traffic-capacity-saturation" || !view.Runnable {
		t.Fatalf("view = %+v", view)
	}
}

func TestQualificationTaskPlannedUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	qualificationTasksRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qualification-tasks/node-failure", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var view srequal.SafeView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Runnable {
		t.Fatalf("planned task must not be runnable: %+v", view)
	}
}

func TestQualificationTaskNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	qualificationTasksRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qualification-tasks/does-not-exist", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestQualificationTaskGitOpsGet(t *testing.T) {
	w := httptest.NewRecorder()
	qualificationTasksRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qualification-tasks/gitops-live-deployment-drift", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var view srequal.SafeView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.ID != "gitops-live-deployment-drift" || view.Runnable {
		t.Fatalf("view = %+v", view)
	}
	raw := strings.ToLower(w.Body.String())
	for _, marker := range []string{"ground_truth", "reference_remediation", "evaluator-only-truth"} {
		if strings.Contains(raw, marker) {
			t.Fatalf("task response leaks evaluator-only marker %q", marker)
		}
	}
}
