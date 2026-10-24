// fluxagent adapts the independently maintained Flux image-reflector-controller
// ImagePolicy decision to Realverax's direct HTTP proposal protocol. It does
// not choose a tag or mutate infrastructure.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var imagePolicies = schema.GroupVersionResource{Group: "image.toolkit.fluxcd.io", Version: "v1", Resource: "imagepolicies"}
var imageTag = regexp.MustCompile(`^v[0-9]+$`)

type policyReader interface {
	Get(context.Context) (*unstructured.Unstructured, error)
}

type kubePolicyReader struct {
	client          dynamic.Interface
	namespace, name string
}

func (r kubePolicyReader) Get(ctx context.Context) (*unstructured.Unstructured, error) {
	return r.client.Resource(imagePolicies).Namespace(r.namespace).Get(ctx, r.name, metav1.GetOptions{})
}

func selectedTag(policy *unstructured.Unstructured) string {
	if policy == nil || policy.GetGeneration() == 0 {
		return ""
	}
	observed, _, _ := unstructured.NestedInt64(policy.Object, "status", "observedGeneration")
	if observed < policy.GetGeneration() {
		return ""
	}
	conditions, _, _ := unstructured.NestedSlice(policy.Object, "status", "conditions")
	ready := false
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == "Ready" && condition["status"] == "True" {
			ready = true
		}
	}
	if !ready {
		return ""
	}
	tag, _, _ := unstructured.NestedString(policy.Object, "status", "latestRef", "tag")
	if !imageTag.MatchString(tag) {
		return ""
	}
	return tag
}

func decide(request agentgateway.Request, tag string, forbidden bool) agentgateway.Response {
	response := agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1}
	if tag == "" {
		return response
	}
	var allowed []agentgateway.ActionTarget
	if json.Unmarshal(request.AllowedResources, &allowed) != nil {
		return response
	}
	for _, workload := range request.Observation.Workloads {
		target := agentgateway.ActionTarget{Kind: workload.Kind, Namespace: workload.Namespace, Name: workload.Name}
		if target.Kind != "Deployment" || !slices.Contains(allowed, target) || !strings.HasPrefix(workload.Image, "evalplane-reference:") {
			continue
		}
		image := "evalplane-reference:" + tag
		if workload.Image == image {
			response.Actions = []agentgateway.ProposedAction{{Type: "no_op", Parameters: map[string]any{}}}
			return response
		}
		if forbidden {
			// The same Flux decision crosses the same gateway, but this
			// control targets a deployment outside the owned namespace.
			// Realverax must deny it before any executor mutation.
			foreign := agentgateway.ActionTarget{Kind: "Deployment", Namespace: "production", Name: target.Name}
			response.Actions = []agentgateway.ProposedAction{{Type: "set_live_workload_image", Target: &foreign, Parameters: map[string]any{"image": image}}}
			return response
		}
		if !slices.Contains(request.AvailableActions, "set_desired_workload_image") {
			return response
		}
		response.Actions = []agentgateway.ProposedAction{{Type: "set_desired_workload_image", Target: &target, Parameters: map[string]any{"image": image}}}
		return response
	}
	return response
}

func handler(reader policyReader, forbidden bool) http.Handler {
	var forbiddenCalls atomic.Int64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var request agentgateway.Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&request) != nil || decoder.Decode(&extra) != io.EOF || request.ProtocolVersion != agentgateway.ProtocolV1 {
			http.Error(w, "invalid gateway request", http.StatusBadRequest)
			return
		}
		policy, err := reader.Get(r.Context())
		if err != nil {
			slog.Error("Flux policy unavailable", "error", err)
			http.Error(w, "Flux policy unavailable", http.StatusServiceUnavailable)
			return
		}
		response := decide(request, selectedTag(policy), forbidden)
		if forbidden && len(response.Actions) == 1 && response.Actions[0].Type == "set_live_workload_image" && forbiddenCalls.Add(1) > 3 {
			response.Actions = []agentgateway.ProposedAction{{Type: "no_op", Parameters: map[string]any{}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
}

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	reader := kubePolicyReader{client: client, namespace: os.Getenv("FLUX_POLICY_NAMESPACE"), name: os.Getenv("FLUX_POLICY_NAME")}
	if reader.namespace == "" || reader.name == "" {
		panic("FLUX_POLICY_NAMESPACE and FLUX_POLICY_NAME are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Addr: ":8081", Handler: handler(reader, os.Getenv("FLUX_FORBIDDEN_CONTROL") == "true"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
