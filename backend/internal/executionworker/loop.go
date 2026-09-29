package executionworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/mcpserver"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"slices"
	"time"
)

func evidenceEvent(runID, kind, code string, payload json.RawMessage) evidence.AppendRunEventInput {
	return evidence.AppendRunEventInput{RunID: runID, EventType: kind, Summary: code, Payload: payload, OccurredAt: time.Now().UTC()}
}
func (w *Worker) checkStop(ctx context.Context, run execution.ExecutionRun) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	owned, err := w.deps.Lifecycle.VerifyRunLease(ctx, run.WorkspaceID, run.RunID, w.config.WorkerID)
	if err != nil {
		return platform("worker_lease_check_failed", err)
	}
	if !owned {
		return ErrLeaseLost
	}
	_, stop, err := w.deps.Lifecycle.RunStatus(ctx, run.WorkspaceID, run.RunID)
	if err != nil {
		return platform("stop_check_failed", err)
	}
	if stop {
		return ErrStopped
	}
	return nil
}
func (w *Worker) observe(ctx context.Context, env observation.EnvironmentContext, code string) error {
	obs, err := w.observeSample(ctx, env)
	if err != nil {
		return err
	}
	payload := map[string]any{"observation": obs}
	if observer, ok := w.deps.Runtime.(GitOpsObserver); ok {
		gitopsEvidence, evidenceErr := observer.ObserveGitOps(ctx, env)
		gitopsEvidence = evidenceForEvent(gitopsEvidence, evidenceErr)
		if gitopsEvidence.ContractVersion != "" || evidenceErr != nil {
			payload["gitops_evidence"] = gitopsEvidence
		}
	}
	return w.event(ctx, env.Identity.RunID, "observation", code, payload)
}

func evidenceForEvent(value gitops.Evidence, err error) gitops.Evidence {
	if err != nil && len(value.Errors) == 0 {
		value.Errors = []string{"gitops_evidence_unavailable"}
	}
	return value
}
func (w *Worker) observeSample(ctx context.Context, env observation.EnvironmentContext) (out agentgateway.Observation, err error) {
	defer func() {
		if recover() != nil {
			err = platform("observation_panic", errors.New("observation provider panicked"))
		}
	}()
	return w.deps.Runtime.Observe(ctx, env)
}
func (w *Worker) agentLoop(ctx context.Context, run execution.ExecutionRun, env observation.EnvironmentContext, view scenario.AgentView, version agent.Version) error {
	var resources []agentgateway.ActionTarget
	if json.Unmarshal(view.AllowedResources, &resources) != nil {
		return platform("invalid_resource_policy", ErrUnsupported)
	}
	for i := range resources {
		if resources[i].Namespace == "" || resources[i].Namespace == "$RUN_NAMESPACE" {
			resources[i].Namespace = env.Environment.Namespace
		}
	}
	view.AllowedResources, _ = json.Marshal(resources)
	var mcpAccess mcpserver.Access
	if version.ToolAccessMode == "mcp" {
		if w.deps.MCP == nil {
			return platform("mcp_tool_access_unavailable", ErrUnsupported)
		}
		var err error
		mcpAccess, err = w.deps.MCP.CreateGrant(ctx, mcpserver.Scope{WorkspaceID: run.WorkspaceID, RunID: run.RunID, Version: version, Environment: env, AgentView: view})
		if err != nil {
			return platform("mcp_grant_creation_failed", err)
		}
		defer func() { mcpAccess.Token = "" }()
	}
	for step := 1; step <= w.config.MaxAgentSteps; step++ {
		if err := w.checkStop(ctx, run); err != nil {
			return err
		}
		if err := w.event(ctx, run.RunID, "system", "agent_step_started", map[string]any{"step": step}); err != nil {
			return err
		}
		obs, err := w.observeSample(ctx, env)
		if err != nil {
			return platform("observation_failed", err)
		}
		if err = w.event(ctx, run.RunID, "observation", "observation_collected", map[string]any{"observation": obs, "step": step}); err != nil {
			return err
		}
		request := agentgateway.Request{ProtocolVersion: agentgateway.ProtocolV1, SessionID: fmt.Sprintf("%s/%d", run.RunID, step), RunID: run.RunID, AgentVersionID: run.Agent.VersionID, Task: agentgateway.AgentTask{Message: view.Task}, VisibleContext: view.VisibleContext, AllowedResources: view.AllowedResources, AvailableActions: view.AllowedActions, Observation: agentObservation(obs, view.AllowedObservations)}
		if version.ToolAccessMode == "mcp" {
			request.ToolAccess = &agentgateway.ToolAccess{Mode: "mcp", Endpoint: mcpAccess.Endpoint, Token: mcpAccess.Token, ExpiresAt: mcpAccess.ExpiresAt}
		}
		if err = w.checkStop(ctx, run); err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, w.config.AgentStepTimeout)
		connection, resolveErr := w.deps.Connection(call, run.WorkspaceID, version)
		if resolveErr != nil {
			cancel()
			return platform("agent_connection_unavailable", resolveErr)
		}
		if validateErr := agentgateway.ValidateConnection(connection); validateErr != nil {
			connection.BearerToken = ""
			cancel()
			return platform("agent_connection_invalid", validateErr)
		}
		response, err := w.deps.Gateway.Invoke(call, connection, request)
		if request.ToolAccess != nil {
			request.ToolAccess.Token = ""
		}
		connection.BearerToken = ""
		timedOut := errors.Is(call.Err(), context.DeadlineExceeded) || errors.Is(err, agentgateway.ErrTimeout)
		cancel()
		if err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			code := "agent_gateway_failed"
			if timedOut {
				code = "agent_timeout"
			} else if errors.Is(err, agentgateway.ErrInvalidResponse) {
				code = "invalid_action_proposal"
			}
			return &Failure{Code: code, Class: "agent", Cause: err}
		}
		if len(response.Actions) > agentgateway.MaxActions || response.ProtocolVersion != agentgateway.ProtocolV1 {
			return &Failure{Code: "invalid_action_proposal", Class: "agent", Cause: agentgateway.ErrInvalidResponse}
		}
		if version.ToolAccessMode == "mcp" && len(response.Actions) != 0 {
			return &Failure{Code: "mcp_duplicate_action_proposal", Class: "agent", Cause: agentgateway.ErrInvalidResponse}
		}
		if err = w.event(ctx, run.RunID, "decision", "agent_decision_received", map[string]any{"step": step, "action_count": len(response.Actions)}); err != nil {
			return err
		}
		useful, terminal := false, false
		for _, action := range response.Actions {
			if err = w.checkStop(ctx, run); err != nil {
				return err
			}
			if action.Type == "no_op" {
				terminal = true
			} else {
				useful = true
			}
			if err = w.processAction(ctx, run, env, view, action); err != nil {
				return err
			}
		}
		w.stage("immediate_outcome_and_post_action_snapshot", "started")
		if err = w.observe(ctx, env, "post_action_observation_collected"); err != nil {
			return platform("observation_failed", err)
		}
		w.stage("immediate_outcome_and_post_action_snapshot", "completed")
		if err = w.event(ctx, run.RunID, "system", "agent_step_completed", map[string]any{"step": step}); err != nil {
			return err
		}
		if terminal || !useful {
			return nil
		} // no_op is the protocol's explicit terminal decision; empty responses preserve the existing contract.
		if step < w.config.MaxAgentSteps {
			if err = wait(ctx, w.config.StepInterval); err != nil {
				return err
			}
		}
	}
	return &Failure{Code: "agent_step_limit_reached", Class: "agent", Cause: ErrAgentStepLimitReached}
}

func (w *Worker) processAction(ctx context.Context, run execution.ExecutionRun, env observation.EnvironmentContext, view scenario.AgentView, action agentgateway.ProposedAction) error {
	// Persist a server-owned whitelist of fields; arbitrary agent parameters and prose
	// may contain credentials and are never copied to evidence.
	proposal := safeProposal(action)
	var targetType, targetRef *string
	var declared []agentgateway.ActionTarget
	_ = json.Unmarshal(view.AllowedResources, &declared)
	if action.Target != nil {
		for _, resource := range declared {
			if resource == *action.Target {
				kind, ref := resource.Kind, resource.Namespace+"/"+resource.Name
				targetType, targetRef = &kind, &ref
				proposal["target"] = resource
				break
			}
		}
	}
	raw, marshalErr := json.Marshal(proposal)
	if marshalErr != nil {
		raw = json.RawMessage(`{"type":"invalid_action_parameters"}`)
	}
	row, err := w.deps.Evidence.RecordAgentAction(ctx, evidence.RecordAgentActionInput{RunID: run.RunID, ActionType: safeActionType(action.Type), TargetType: targetType, TargetRef: targetRef, Status: "proposed", RequestPayload: raw, ResultPayload: json.RawMessage(`{}`)})
	if err != nil {
		return platform("action_persistence_failed", err)
	}
	decision := policy(view, action)
	if marshalErr != nil {
		decision = actionexec.ActionResult{Status: "denied", Code: "invalid_action_parameters"}
	}
	if action.Target != nil && action.Target.Namespace != env.Environment.Namespace {
		decision = actionexec.ActionResult{ActionType: safeActionType(action.Type), Status: "denied", Code: "cross_namespace"}
	}
	if decision.Code == "" {
		decision = w.deps.Actions.Authorize(ctx, env, action)
	}
	if decision.Status == "failed" {
		final, cancel := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
		defer cancel()
		decision.Authorized = false
		decision.Code = "authorization_unavailable"
		return errors.Join(platform("action_authorization_unavailable", errors.New("resource verification failed")), w.persistAction(final, run.RunID, row.ID, decision))
	}
	if decision.Status != "denied" && (!decision.Authorized || decision.Status != "allowed") {
		return platform("invalid_authorization_result", errors.New("invalid executor response"))
	}
	// Preserve an authorization decision even if stop raced with its read-only
	// checks. The next stop check still prevents execution after cancellation.
	decisionCtx, decisionCancel := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
	err = w.persistAction(decisionCtx, run.RunID, row.ID, decision)
	decisionCancel()
	if err != nil {
		return err
	}
	if decision.Status == "denied" {
		return nil
	}
	if err = w.checkStop(ctx, run); err != nil {
		final, cancel := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
		defer cancel()
		return errors.Join(err, w.persistAction(final, run.RunID, row.ID, actionexec.ActionResult{Status: "failed", Authorized: true, Code: "action_cancelled"}))
	}
	w.stage("action_execution", "started")
	result := w.deps.Actions.ExecuteAction(ctx, env, action)
	// Persist the outcome even when a cancellation races with a Kubernetes request.
	final, cancel := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
	defer cancel()
	if err = w.persistAction(final, run.RunID, row.ID, result); err != nil {
		return err
	}
	if result.Status == "failed" {
		w.stage("action_execution", "failed")
	} else {
		w.stage("action_execution", "completed")
	}
	if result.Status == "failed" {
		return platform("action_execution_failed", errors.New("controlled action failed"))
	}
	return nil
}
func (w *Worker) persistAction(ctx context.Context, runID, actionID string, result actionexec.ActionResult) error {
	if result.Attribution != nil {
		result.Attribution.RunID = runID
		result.Attribution.ActionID = actionID
	}
	payload, _ := json.Marshal(map[string]any{"status": result.Status, "authorized": result.Authorized, "executed": result.Executed, "code": result.Code, "attribution": result.Attribution})
	if err := w.deps.Evidence.UpdateAgentAction(ctx, evidence.UpdateActionInput{RunID: runID, ActionID: actionID, Status: result.Status, Authorized: result.Authorized, ResultPayload: payload}); err != nil {
		return platform("action_persistence_failed", err)
	}
	code := "action_" + result.Status
	if result.Status == "allowed" {
		code = "action_authorized"
	}
	kind := "action"
	if result.Status == "denied" {
		kind = "safety"
	}
	return w.event(ctx, runID, kind, code, map[string]any{"action_id": actionID, "status": result.Status, "authorized": result.Authorized, "denial_code": result.Code})
}
func safeActionType(value string) string {
	switch value {
	case "inspect", "no_op", "scale_workload", "restart_pod", "set_workload_image":
		return value
	}
	return "unsupported_action"
}
func safeProposal(action agentgateway.ProposedAction) map[string]any {
	out := map[string]any{"type": safeActionType(action.Type)}
	if action.Type == "scale_workload" {
		switch n := action.Parameters["replicas"].(type) {
		case float64:
			out["replicas"] = n
		case int:
			out["replicas"] = n
		}
	}
	if action.Type == "set_workload_image" {
		if image, ok := action.Parameters["image"].(string); ok && len(image) <= 255 {
			out["image"] = image
		}
	}
	return out
}
func policy(view scenario.AgentView, action agentgateway.ProposedAction) actionexec.ActionResult {
	deny := func(code string) actionexec.ActionResult {
		return actionexec.ActionResult{ActionType: safeActionType(action.Type), Status: "denied", Code: code}
	}
	if safeActionType(action.Type) == "unsupported_action" {
		return deny("unsupported_action")
	}
	if !slices.Contains(view.AllowedActions, action.Type) {
		return deny("unauthorized_action")
	}
	if action.Type == "no_op" {
		if action.Target != nil || len(action.Parameters) > 0 {
			return deny("invalid_action_parameters")
		}
		return actionexec.ActionResult{}
	}
	if action.Type == "inspect" && action.Target == nil {
		return actionexec.ActionResult{}
	}
	if action.Target == nil {
		return deny("invalid_action_parameters")
	}
	var resources []agentgateway.ActionTarget
	if json.Unmarshal(view.AllowedResources, &resources) != nil {
		return deny("disallowed_resource")
	}
	for _, resource := range resources {
		if resource == *action.Target {
			return actionexec.ActionResult{}
		}
	}
	return deny("disallowed_resource")
}

// Observations are provider-owned. Kubernetes event messages are excluded because
// they can expose traffic injection details; only explicitly allowed channels cross.
func agentObservation(obs agentgateway.Observation, allowed []string) agentgateway.Observation {
	return agentgateway.ProjectObservation(obs, allowed)
}
