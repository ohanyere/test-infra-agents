// referenceagent is an E2E fixture, not a production infrastructure agent.
// It proves gateway/action plumbing, not the competence of arbitrary AI agents.
// It never receives criteria, accesses Kubernetes, or grades its own decisions.
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
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func decide(request agentgateway.Request) agentgateway.Response {
	action := agentgateway.ProposedAction{Type: "inspect", Parameters: map[string]any{}}
	errorRate, known := request.Observation.Metrics["error_rate_pct"].(float64)
	var targets []agentgateway.ActionTarget
	_ = json.Unmarshal(request.AllowedResources, &targets)
	for _, workload := range request.Observation.Workloads {
		target := agentgateway.ActionTarget{Kind: workload.Kind, Namespace: workload.Namespace, Name: workload.Name}
		if workload.Kind != "Deployment" || !slices.Contains(targets, target) {
			continue
		}
		// Fixed conservative policy belongs to this fixture, not the evaluator.
		if known && errorRate > 0 && strings.HasSuffix(workload.Image, ":v18") && slices.Contains(request.AvailableActions, "set_workload_image") {
			action = agentgateway.ProposedAction{Type: "set_workload_image", Target: &target, Parameters: map[string]any{"image": strings.TrimSuffix(workload.Image, ":v18") + ":v17"}}
		} else if known && errorRate > 0 && workload.DesiredReplicas < 4 && slices.Contains(request.AvailableActions, "scale_workload") {
			action = agentgateway.ProposedAction{Type: "scale_workload", Target: &target, Parameters: map[string]any{"replicas": 4}}
		} else if known && errorRate == 0 && strings.HasSuffix(workload.Image, ":v17") {
			action.Type = "no_op"
		} else if known && errorRate == 0 && workload.DesiredReplicas >= 4 && workload.AvailableReplicas >= workload.DesiredReplicas {
			action.Type = "no_op"
		}
		break
	}
	if !slices.Contains(request.AvailableActions, action.Type) {
		return agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, Actions: []agentgateway.ProposedAction{}}
	}
	return agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, Actions: []agentgateway.ProposedAction{action}}
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
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
	// Log IDs only. The live harness captures request bodies separately, after
	// checking the AgentView boundary; tokens and arbitrary agent context stay out.
	slog.Info("reference decision", "run_id", request.RunID, "agent_version_id", request.AgentVersionID, "session_id", request.SessionID)
	if request.ToolAccess != nil {
		if err := useMCP(r.Context(), request); err != nil {
			http.Error(w, "MCP tool interaction failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, ReasoningSummary: "completed through evaluation-scoped MCP tools", Actions: []agentgateway.ProposedAction{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(decide(request))
}

type grantTransport struct {
	token string
	base  http.RoundTripper
}

func (t grantTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func useMCP(ctx context.Context, request agentgateway.Request) error {
	access := request.ToolAccess
	if access == nil || access.Mode != "mcp" {
		return errors.New("missing MCP access")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "evalplane-reference-agent", Version: "stage24-v1"}, nil)
	httpClient := &http.Client{Transport: grantTransport{token: access.Token, base: http.DefaultTransport}, Timeout: 10 * time.Second}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: access.Endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	available := map[string]bool{}
	for _, tool := range listed.Tools {
		available[tool.Name] = true
	}
	if !available[mcpserver.ToolObserveWorkloads] || !available[mcpserver.ToolGetTelemetry] {
		return errors.New("required observation tools unavailable")
	}
	workloads, err := callOutput(ctx, session, mcpserver.ToolObserveWorkloads, map[string]any{})
	if err != nil {
		return err
	}
	telemetry, err := callOutput(ctx, session, mcpserver.ToolGetTelemetry, map[string]any{})
	if err != nil {
		return err
	}
	errorRate, measured := telemetry.Metrics["error_rate_pct"].(float64)
	if measured && errorRate > 0 && available[mcpserver.ToolScaleWorkload] {
		for _, workload := range workloads.Workloads {
			if workload.Kind == "Deployment" && workload.DesiredReplicas < 4 {
				_, err = callOutput(ctx, session, mcpserver.ToolScaleWorkload, map[string]any{"kind": workload.Kind, "namespace": workload.Namespace, "name": workload.Name, "replicas": 4})
				return err
			}
		}
	}
	if available[mcpserver.ToolNoOp] {
		_, err = callOutput(ctx, session, mcpserver.ToolNoOp, map[string]any{})
	}
	return err
}

func callOutput(ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) (mcpserver.ToolOutput, error) {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result == nil || result.IsError {
		if err == nil {
			err = errors.New("MCP tool returned an error")
		}
		return mcpserver.ToolOutput{}, err
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return mcpserver.ToolOutput{}, err
	}
	var out mcpserver.ToolOutput
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
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
		slog.Error("reference agent stopped")
		os.Exit(1)
	}
}
