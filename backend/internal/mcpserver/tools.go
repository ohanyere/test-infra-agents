package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
)

type authenticated struct {
	grant Grant
	scope Scope
}

type authContextKey struct{}

func contextAuth(ctx context.Context) (authenticated, error) {
	value, ok := ctx.Value(authContextKey{}).(authenticated)
	if !ok {
		return authenticated{}, ErrInvalidToken
	}
	return value, nil
}

func (s *Service) observeTool(ctx context.Context, tool string) (ToolOutput, error) {
	authenticated, err := contextAuth(ctx)
	if err != nil {
		return ToolOutput{}, err
	}
	if !slices.Contains(s.effectiveTools(authenticated.scope), tool) {
		s.record(ctx, authenticated.scope.RunID, "mcp_tool_denied", tool, map[string]any{"denial_code": "unauthorized_action"})
		return ToolOutput{Status: "denied", Code: "unauthorized_action"}, errors.New("tool is not permitted for this run")
	}
	observation, err := s.observer.Observe(ctx, authenticated.scope)
	if err != nil {
		s.record(ctx, authenticated.scope.RunID, "mcp_tool_failed", tool, map[string]any{"reason_code": "observation_unavailable"})
		return ToolOutput{}, errors.New("observation unavailable")
	}
	projected := agentgateway.ProjectObservation(observation, authenticated.scope.AgentView.AllowedObservations)
	out := ToolOutput{Status: "succeeded"}
	switch tool {
	case ToolObserveWorkloads:
		out.Workloads = projected.Workloads
	case ToolObservePods:
		out.Pods = projected.Pods
	case ToolObserveEvents:
		out.Events = projected.RecentEvents
	case ToolGetTelemetry:
		out.Metrics = projected.Metrics
		available := false
		if value, ok := projected.Metrics["telemetry_available"].(bool); ok {
			available = value
		}
		out.Available = &available
	}
	s.record(ctx, authenticated.scope.RunID, "mcp_tool_succeeded", tool, nil)
	return out, nil
}

func (s *Service) actionTool(ctx context.Context, tool string, action agentgateway.ProposedAction) (ToolOutput, error) {
	authenticated, err := contextAuth(ctx)
	if err != nil {
		return ToolOutput{}, err
	}
	if !slices.Contains(s.effectiveTools(authenticated.scope), tool) {
		s.record(ctx, authenticated.scope.RunID, "mcp_tool_denied", tool, map[string]any{"denial_code": "unauthorized_action"})
		return ToolOutput{Status: "denied", Code: "unauthorized_action"}, errors.New("tool is not permitted for this run")
	}
	requestPayload := map[string]any{"type": action.Type}
	var targetType, targetRef *string
	if action.Target != nil {
		requestPayload["target"] = action.Target
		kind, ref := action.Target.Kind, action.Target.Namespace+"/"+action.Target.Name
		targetType, targetRef = &kind, &ref
	}
	if action.Type == "scale_workload" {
		requestPayload["replicas"] = action.Parameters["replicas"]
	}
	raw, _ := json.Marshal(requestPayload)
	row, err := s.evidence.RecordAgentAction(ctx, evidence.RecordAgentActionInput{RunID: authenticated.scope.RunID, ActionType: action.Type, TargetType: targetType, TargetRef: targetRef, Status: "proposed", RequestPayload: raw, ResultPayload: json.RawMessage(`{}`)})
	if err != nil {
		return ToolOutput{}, errors.New("action persistence unavailable")
	}
	decision := s.policy(authenticated.scope, action)
	if decision.Code == "" {
		decision = s.actions.Authorize(ctx, authenticated.scope, action)
	}
	if decision.Status != "allowed" || !decision.Authorized {
		if decision.Code == "" {
			decision.Code = "authorization_unavailable"
		}
		decision.Status = "denied"
		_ = s.persistAction(ctx, authenticated.scope.RunID, row.ID, decision)
		s.record(ctx, authenticated.scope.RunID, "mcp_tool_denied", tool, map[string]any{"denial_code": decision.Code})
		return ToolOutput{Status: "denied", Code: decision.Code}, errors.New("action denied")
	}
	if err = s.persistAction(ctx, authenticated.scope.RunID, row.ID, decision); err != nil {
		return ToolOutput{}, err
	}
	s.record(ctx, authenticated.scope.RunID, "mcp_tool_authorized", tool, nil)
	result := s.actions.Execute(ctx, authenticated.scope, action)
	if err = s.persistAction(ctx, authenticated.scope.RunID, row.ID, result); err != nil {
		return ToolOutput{}, err
	}
	if result.Status != "executed" || !result.Executed {
		s.record(ctx, authenticated.scope.RunID, "mcp_tool_failed", tool, map[string]any{"reason_code": result.Code})
		return ToolOutput{Status: "failed", Code: result.Code}, errors.New("controlled action failed")
	}
	s.record(ctx, authenticated.scope.RunID, "mcp_tool_succeeded", tool, nil)
	return ToolOutput{Status: "executed"}, nil
}

func (s *Service) policy(scope Scope, action agentgateway.ProposedAction) actionexec.ActionResult {
	deny := func(code string) actionexec.ActionResult {
		return actionexec.ActionResult{ActionType: action.Type, Status: "denied", Code: code}
	}
	if !slices.Contains(scope.AgentView.AllowedActions, action.Type) || !slices.Contains(scope.Version.Capabilities, action.Type) {
		return deny("unauthorized_action")
	}
	if action.Type == "no_op" {
		if action.Target != nil || len(action.Parameters) != 0 {
			return deny("invalid_action_parameters")
		}
		return actionexec.ActionResult{}
	}
	if action.Target == nil {
		return deny("invalid_action_parameters")
	}
	if action.Target.Namespace != scope.Environment.Environment.Namespace {
		return deny("cross_namespace")
	}
	var resources []agentgateway.ActionTarget
	if json.Unmarshal(scope.AgentView.AllowedResources, &resources) != nil {
		return deny("disallowed_resource")
	}
	for _, resource := range resources {
		if resource.Namespace == "$RUN_NAMESPACE" || resource.Namespace == "" {
			resource.Namespace = scope.Environment.Environment.Namespace
		}
		if resource == *action.Target {
			return actionexec.ActionResult{}
		}
	}
	return deny("disallowed_resource")
}

func (s *Service) persistAction(ctx context.Context, runID, actionID string, result actionexec.ActionResult) error {
	payload, _ := json.Marshal(map[string]any{"status": result.Status, "authorized": result.Authorized, "executed": result.Executed, "code": result.Code})
	return s.evidence.UpdateAgentAction(ctx, evidence.UpdateActionInput{RunID: runID, ActionID: actionID, Status: result.Status, Authorized: result.Authorized, ResultPayload: payload})
}

func (s *Service) debugScopeCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.scopes)
}
