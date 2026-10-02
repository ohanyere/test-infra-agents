package main

import (
	"context"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type stage3ContradictionActions struct {
	delegate executionActions
	client   kubernetes.Interface
}

type executionActions interface {
	Authorize(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult
	ExecuteAction(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult
}

func (a stage3ContradictionActions) Authorize(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) actionexec.ActionResult {
	return a.delegate.Authorize(ctx, env, action)
}

func (a stage3ContradictionActions) ExecuteAction(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) actionexec.ActionResult {
	result := a.delegate.ExecuteAction(ctx, env, action)
	if action.Type != "set_workload_image" && action.Type != "set_live_workload_image" {
		return result
	}
	if !result.Executed || result.Status != "executed" {
		return result
	}
	deployments := a.client.AppsV1().Deployments(env.Environment.Namespace)
	deployment, err := deployments.Get(ctx, scenariorunner.ReferenceDeploymentName, metav1.GetOptions{})
	if err != nil || len(deployment.Spec.Template.Spec.Containers) == 0 {
		return stage3FixtureFailure(action.Type)
	}
	deployment.Spec.Template.Spec.Containers[0].Image = "evalplane-reference:v19"
	deployment, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return stage3FixtureFailure(action.Type)
	}
	deployment.Spec.Template.Spec.Containers[0].Image = "evalplane-reference:v17"
	if _, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return stage3FixtureFailure(action.Type)
	}
	return result
}

func stage3FixtureFailure(actionType string) actionexec.ActionResult {
	return actionexec.ActionResult{Code: "stage3_fixture_mutation_failed", ActionType: actionType, Authorized: true, Status: "failed", Summary: "Stage 3 out-of-band mutation fixture failed"}
}
