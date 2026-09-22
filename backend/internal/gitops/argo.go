package gitops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var applications = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

type ArgoReader struct {
	Client                 dynamic.Interface
	Namespace, Application string
	Now                    func() time.Time
}

func (r ArgoReader) Read(ctx context.Context) (ArgoState, error) {
	return r.read(ctx, false)
}

// ReadWithDiagnostics preserves the Kubernetes read error for trusted runtime
// diagnostics. Read keeps its generic verifier-facing error contract.
func (r ArgoReader) ReadWithDiagnostics(ctx context.Context) (ArgoState, error) {
	return r.read(ctx, true)
}

func (r ArgoReader) read(ctx context.Context, diagnostic bool) (ArgoState, error) {
	if r.Client == nil || strings.TrimSpace(r.Namespace) == "" || strings.TrimSpace(r.Application) == "" {
		return ArgoState{}, errors.New("Argo application identity is required")
	}
	app, err := r.Client.Resource(applications).Namespace(r.Namespace).Get(ctx, r.Application, metav1.GetOptions{})
	if err != nil {
		if diagnostic {
			return ArgoState{}, fmt.Errorf("get Argo Application %s/%s: %w", r.Namespace, r.Application, err)
		}
		return ArgoState{}, errors.New("Argo application evidence unavailable")
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	if syncStatus != "Synced" && syncStatus != "OutOfSync" {
		syncStatus = "Unknown"
	}
	health, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	target, _, _ := unstructured.NestedString(app.Object, "spec", "source", "targetRevision")
	observed, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	_, automated, _ := unstructured.NestedMap(app.Object, "spec", "syncPolicy", "automated")
	selfHeal, _, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "selfHeal")
	return ArgoState{Application: r.Application, Namespace: r.Namespace, SyncStatus: syncStatus, HealthStatus: health, TargetRevision: target, ObservedRevision: observed, Automated: automated, SelfHeal: selfHeal, CapturedAt: now().UTC(), Reference: "argo-application:" + r.Namespace + "/" + r.Application + ":rv:" + app.GetResourceVersion()}, nil
}
