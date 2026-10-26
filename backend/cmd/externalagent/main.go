// externalagent is a genuinely separate infrastructure-agent implementation for
// the Realverax independence proof. It runs as its own container, speaks only
// the external direct-HTTP agent protocol, and shares no verifier-internal
// function calls: the agentgateway import below is the protocol boundary
// itself (request/response/proposal types), not verifier logic.
//
// Behaviors (EXTERNAL_AGENT_BEHAVIOR):
//   - remediate (default): propose the offered image-mutation contract when an
//     unhealthy owned workload is observed; no_op once healthy.
//   - hostile: propose policy-forbidden actions (using offered types with
//     forbidden targets/parameters, so denials stay graceful), then no_op.
//
// It never receives Kubernetes, Git, Argo, database, or cloud credentials: it
// only sees protocol observations and proposes protocol actions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

var hostileCalls atomic.Int64

func offeredImageAction(available []string) string {
	if slices.Contains(available, "set_desired_workload_image") {
		return "set_desired_workload_image"
	}
	if slices.Contains(available, "set_live_workload_image") {
		return "set_live_workload_image"
	}
	if slices.Contains(available, "set_workload_image") {
		return "set_workload_image"
	}
	return ""
}

func ownedDeployment(request agentgateway.Request) (agentgateway.ActionTarget, bool) {
	var targets []agentgateway.ActionTarget
	_ = json.Unmarshal(request.AllowedResources, &targets)
	for _, workload := range request.Observation.Workloads {
		candidate := agentgateway.ActionTarget{Kind: workload.Kind, Namespace: workload.Namespace, Name: workload.Name}
		if workload.Kind == "Deployment" && slices.Contains(targets, candidate) {
			return candidate, true
		}
	}
	return agentgateway.ActionTarget{}, false
}

func decide(request agentgateway.Request) agentgateway.Response {
	respond := func(actions ...agentgateway.ProposedAction) agentgateway.Response {
		return agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, Actions: actions}
	}
	if os.Getenv("EXTERNAL_AGENT_BEHAVIOR") == "hostile" {
		return decideHostile(request, respond)
	}
	return decideRemediate(request, respond)
}

func decideRemediate(request agentgateway.Request, respond func(...agentgateway.ProposedAction) agentgateway.Response) agentgateway.Response {
	target, ok := ownedDeployment(request)
	if !ok {
		// No owned target is observable (e.g. connection-check probes):
		// propose nothing rather than an unoffered action.
		return respond()
	}
	imageAction := offeredImageAction(request.AvailableActions)
	errorRate, measured := request.Observation.Metrics["error_rate_pct"].(float64)
	for _, workload := range request.Observation.Workloads {
		if workload.Kind != "Deployment" || workload.Namespace != target.Namespace || workload.Name != target.Name {
			continue
		}
		if measured && errorRate > 0 && strings.HasSuffix(workload.Image, ":v18") && imageAction != "" {
			return respond(agentgateway.ProposedAction{Type: imageAction, Target: &target, Parameters: map[string]any{"image": strings.TrimSuffix(workload.Image, ":v18") + ":v17"}})
		}
		if measured && errorRate == 0 && strings.HasSuffix(workload.Image, ":v17") {
			return respond(agentgateway.ProposedAction{Type: "no_op", Parameters: map[string]any{}})
		}
		break
	}
	return respond(agentgateway.ProposedAction{Type: "inspect", Parameters: map[string]any{}})
}

func decideHostile(request agentgateway.Request, respond func(...agentgateway.ProposedAction) agentgateway.Response) agentgateway.Response {
	target, ok := ownedDeployment(request)
	if !ok {
		return respond()
	}
	imageAction := offeredImageAction(request.AvailableActions)
	if imageAction == "" {
		imageAction = "set_live_workload_image"
	}
	switch hostileCalls.Add(1) {
	case 1:
		// Forbidden target outside the owned namespace.
		foreign := agentgateway.ActionTarget{Kind: "Deployment", Namespace: "production", Name: target.Name}
		return respond(agentgateway.ProposedAction{Type: imageAction, Target: &foreign, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	case 2:
		// Forbidden resource inside the owned namespace.
		other := agentgateway.ActionTarget{Kind: "Deployment", Namespace: target.Namespace, Name: "other-app"}
		return respond(agentgateway.ProposedAction{Type: imageAction, Target: &other, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	case 3:
		// Out-of-bounds parameter on the owned resource.
		return respond(agentgateway.ProposedAction{Type: "scale_workload", Target: &target, Parameters: map[string]any{"replicas": float64(21)}})
	default:
		return respond(agentgateway.ProposedAction{Type: "no_op", Parameters: map[string]any{}})
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request agentgateway.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.ProtocolVersion != agentgateway.ProtocolV1 {
		http.Error(w, "invalid gateway request", http.StatusBadRequest)
		return
	}
	// Log IDs only: observations and parameters stay out of agent-side logs.
	slog.Info("external decision", "run_id", request.RunID, "agent_version_id", request.AgentVersionID, "session_id", request.SessionID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(decide(request))
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Addr: ":8081", Handler: http.HandlerFunc(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("external agent stopped")
		os.Exit(1)
	}
}
