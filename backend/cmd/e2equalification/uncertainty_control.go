package main

import (
	"context"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/executionworker"
	"github.com/evalplane/evalplane/backend/internal/observation"
)

// uncertaintyControlActions introduces a verifier evidence outage only after a
// legitimate mutating action has completed. Authorization, mutation, and action
// persistence remain owned by the normal executor and worker paths.
type uncertaintyControlActions struct {
	delegate executionworker.Actions
	runtime  *runtime
}

func (a uncertaintyControlActions) Authorize(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) actionexec.ActionResult {
	return a.delegate.Authorize(ctx, env, action)
}

func (a uncertaintyControlActions) ExecuteAction(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) actionexec.ActionResult {
	result := a.delegate.ExecuteAction(ctx, env, action)
	if action.Type == "set_workload_image" && result.Authorized && result.Executed && result.Status == "executed" {
		a.runtime.evidenceUnavailable.Store(true)
	}
	return result
}
