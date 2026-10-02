package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/executionworker"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var fixtureRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type fixtureGitState struct {
	Revision string `json:"revision"`
	Image    string `json:"image"`
}

func readFixtureState(ctx context.Context, client *http.Client, endpoint string) (fixtureGitState, error) {
	if client == nil || strings.TrimSpace(endpoint) == "" {
		return fixtureGitState{}, errors.New("Git fixture state endpoint is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fixtureGitState{}, errors.New("build Git fixture state request")
	}
	response, err := client.Do(req)
	if err != nil {
		return fixtureGitState{}, errors.New("read Git fixture state")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fixtureGitState{}, fmt.Errorf("read Git fixture state: HTTP %d", response.StatusCode)
	}
	var state fixtureGitState
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return fixtureGitState{}, errors.New("decode Git fixture state")
	}
	if !fixtureRevisionPattern.MatchString(state.Revision) {
		return fixtureGitState{}, errors.New("Git fixture returned an invalid revision")
	}
	if state.Image != "evalplane-reference:v18" && state.Image != "evalplane-reference:v17" {
		return fixtureGitState{}, errors.New("Git fixture returned an unsupported desired image")
	}
	return state, nil
}

func (r *runtime) readFixtureDesiredState(ctx context.Context, env observation.EnvironmentContext) (gitops.DesiredState, error) {
	client := r.gitHTTP
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	}
	state, err := readFixtureState(ctx, client, r.gitStateURL)
	if err != nil {
		return gitops.DesiredState{}, err
	}
	return gitops.DesiredState{
		Repository: safeFixtureRepository(r.gitRepo),
		Revision:   state.Revision,
		Path:       "desired/checkout.yaml",
		Resource:   "Deployment/" + env.Environment.Namespace + "/" + scenariorunner.ReferenceDeploymentName,
		Container:  "reference-app",
		Image:      state.Image,
		CapturedAt: time.Now().UTC(),
		Reference:  "git:" + state.Revision + ":desired/checkout.yaml",
	}, nil
}

func safeFixtureRepository(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "repository"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

type positiveControlActions struct {
	delegate executionworker.Actions
	runtime  *runtime
	client   *http.Client
	deadline time.Duration
}

func (a positiveControlActions) Authorize(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) actionexec.ActionResult {
	return a.delegate.Authorize(ctx, env, action)
}

func (a positiveControlActions) ExecuteAction(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) actionexec.ActionResult {
	// Only the explicit desired-state contract takes the Git path. Live
	// mutation types (including the legacy identifier, which keeps the
	// direct-live contract) always dispatch to the live executor below.
	if action.Type != "set_desired_workload_image" {
		return a.delegate.ExecuteAction(ctx, env, action)
	}
	decision := a.delegate.Authorize(ctx, env, action)
	if decision.Status != "allowed" || !decision.Authorized {
		return decision
	}
	image, _ := action.Parameters["image"].(string)
	if image != "evalplane-reference:v17" {
		return actionexec.ActionResult{Code: "invalid_action_parameters", ActionType: action.Type, Status: "denied", Summary: "durable controls only permit the declared v17 remediation"}
	}
	deadline := a.deadline
	if deadline <= 0 {
		deadline = 3 * time.Minute
	}
	actionCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(actionCtx, http.MethodPost, a.runtime.gitUpdateURL, nil)
	if err != nil {
		return positiveActionFailure(action.Type, "build desired-state update request")
	}
	response, err := a.client.Do(req)
	if err != nil {
		return positiveActionFailure(action.Type, "update authoritative desired state")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return positiveActionFailure(action.Type, fmt.Sprintf("update authoritative desired state: HTTP %d", response.StatusCode))
	}
	if err := a.waitForReconciliation(actionCtx, env, image); err != nil {
		return positiveActionFailure(action.Type, "wait for authoritative desired state reconciliation")
	}
	return actionexec.ActionResult{ActionType: action.Type, Authorized: true, Executed: true, Status: "executed", Summary: "updated Git desired image to v17 and observed Argo reconciliation"}
}

func (a positiveControlActions) waitForReconciliation(ctx context.Context, env observation.EnvironmentContext, image string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		state, stateErr := readFixtureState(ctx, a.client, a.runtime.gitStateURL)
		argo, argoErr := (gitops.ArgoReader{Client: a.runtime.dynamic, Namespace: argoNamespace, Application: a.runtime.applicationName()}).Read(ctx)
		deployment, deploymentErr := a.runtime.client.AppsV1().Deployments(env.Environment.Namespace).Get(ctx, scenariorunner.ReferenceDeploymentName, metav1.GetOptions{})
		if stateErr == nil && argoErr == nil && deploymentErr == nil && state.Image == image && argo.SyncStatus == "Synced" && argo.ObservedRevision == state.Revision && deployment.Generation <= deployment.Status.ObservedGeneration && len(deployment.Spec.Template.Spec.Containers) > 0 && deployment.Spec.Template.Spec.Containers[0].Image == image {
			desired := int32(1)
			if deployment.Spec.Replicas != nil {
				desired = *deployment.Spec.Replicas
			}
			if deployment.Status.UpdatedReplicas == desired && deployment.Status.AvailableReplicas == desired {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func positiveActionFailure(actionType, summary string) actionexec.ActionResult {
	return actionexec.ActionResult{Code: "action_execution_failed", ActionType: actionType, Authorized: true, Status: "failed", Summary: summary}
}
