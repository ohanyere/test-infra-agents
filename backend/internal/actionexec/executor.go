package actionexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const MaxActionsPerStep = 10

var ErrTooManyActions = errors.New("too many actions")

type ActionResult struct {
	Code       string `json:"code,omitempty"`
	ActionType string `json:"action_type"`
	Authorized bool   `json:"authorized"`
	Executed   bool   `json:"executed"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
}
type Config struct{ MaxReplicas int32 }

func DefaultConfig() Config { return Config{MaxReplicas: 20} }

type EnvironmentVerifier interface {
	GetEnvironment(context.Context, string) (kubeenv.Environment, error)
}
type EventSink interface {
	AppendRunEvent(context.Context, evidence.AppendRunEventInput) (evidence.RunEvent, error)
}
type Executor struct {
	client   kubernetes.Interface
	verifier EnvironmentVerifier
	config   Config
	events   EventSink
}

func New(client kubernetes.Interface, verifier EnvironmentVerifier, config Config, events EventSink) (*Executor, error) {
	if client == nil || verifier == nil {
		return nil, errors.New("Kubernetes client and environment verifier are required")
	}
	if config.MaxReplicas < 1 {
		return nil, errors.New("max replicas must be positive")
	}
	return &Executor{client: client, verifier: verifier, config: config, events: events}, nil
}

func (e *Executor) Execute(ctx context.Context, env observation.EnvironmentContext, actions []agentgateway.ProposedAction) ([]ActionResult, error) {
	if len(actions) > MaxActionsPerStep {
		return nil, ErrTooManyActions
	}
	results := make([]ActionResult, 0, len(actions))
	for _, action := range actions {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		result := e.executeOne(ctx, env, action, false)
		results = append(results, result)
		e.record(ctx, env.Identity.RunID, result)
	}
	return results, nil
}

// Authorize checks current ownership and parameters without changing resources.
func (e *Executor) Authorize(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) ActionResult {
	return e.executeOne(ctx, env, action, true)
}

// ExecuteAction rechecks authorization immediately before mutation; the worker owns evidence.
func (e *Executor) ExecuteAction(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction) ActionResult {
	return e.executeOne(ctx, env, action, false)
}

func (e *Executor) executeOne(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction, dryRun bool) ActionResult {
	deny := func(code, summary string) ActionResult {
		return ActionResult{Code: code, ActionType: action.Type, Status: "denied", Summary: summary}
	}
	if ctx.Err() != nil {
		return failed(action.Type, "action cancelled")
	}
	if action.Type != "inspect" && action.Type != "no_op" && action.Type != "scale_workload" && action.Type != "restart_pod" {
		return deny("unsupported_action", "unsupported action denied")
	}
	verified, err := e.verifier.GetEnvironment(ctx, env.Identity.RunID)
	if err != nil && !errors.Is(err, kubeenv.ErrEnvironmentOwnership) {
		return failed(action.Type, "environment verification unavailable")
	}
	if err != nil || verified.Namespace != env.Environment.Namespace || env.Environment.RunID != env.Identity.RunID {
		return deny("unmanaged_resource", "environment ownership verification failed")
	}
	if action.Type == "inspect" || action.Type == "no_op" {
		if action.Type == "no_op" && (action.Target != nil || len(action.Parameters) > 0) {
			return deny("invalid_action_parameters", "no_op cannot specify parameters or a target")
		}
		if dryRun {
			return ActionResult{ActionType: action.Type, Authorized: true, Status: "allowed"}
		}
		return ActionResult{ActionType: action.Type, Authorized: true, Executed: true, Status: "executed", Summary: action.Type + " completed without mutation"}
	}
	if action.Target == nil {
		return deny("invalid_action_parameters", "missing target")
	}
	if action.Target.Namespace != verified.Namespace {
		return deny("cross_namespace", "cross-namespace target denied")
	}
	if action.Type == "scale_workload" {
		return e.scale(ctx, env, action, deny, dryRun)
	}
	return e.restart(ctx, env, action, deny, dryRun)
}

func (e *Executor) scale(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction, deny func(string, string) ActionResult, dryRun bool) ActionResult {
	if action.Target.Kind != "Deployment" || action.Target.Name != scenariorunner.ReferenceDeploymentName {
		return deny("disallowed_resource", "only the reference Deployment may be scaled")
	}
	replicas, ok := replicaValue(action.Parameters["replicas"])
	if !ok || replicas < 1 || replicas > e.config.MaxReplicas {
		return deny("invalid_action_parameters", "replica count outside allowed bounds")
	}
	d, err := e.client.AppsV1().Deployments(env.Environment.Namespace).Get(ctx, action.Target.Name, metav1.GetOptions{})
	if err != nil {
		return failed(action.Type, "Kubernetes deployment lookup failed")
	}
	if !ownedReference(d.Labels, env.Identity) {
		return deny("unmanaged_resource", "deployment ownership verification failed")
	}
	if dryRun {
		return ActionResult{ActionType: action.Type, Authorized: true, Status: "allowed"}
	}
	if ctx.Err() != nil {
		return failed(action.Type, "action cancelled")
	}
	old := int32(0)
	if d.Spec.Replicas != nil {
		old = *d.Spec.Replicas
	}
	d.Spec.Replicas = &replicas
	if _, err = e.client.AppsV1().Deployments(env.Environment.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return failed(action.Type, "Kubernetes deployment update failed")
	}
	return ActionResult{ActionType: action.Type, Authorized: true, Executed: true, Status: "executed", Summary: fmt.Sprintf("scaled %s from %d to %d replicas", d.Name, old, replicas)}
}

func (e *Executor) restart(ctx context.Context, env observation.EnvironmentContext, action agentgateway.ProposedAction, deny func(string, string) ActionResult, dryRun bool) ActionResult {
	if action.Target.Kind != "Pod" {
		return deny("invalid_action_parameters", "restart target must be a Pod")
	}
	p, err := e.client.CoreV1().Pods(env.Environment.Namespace).Get(ctx, action.Target.Name, metav1.GetOptions{})
	if err != nil {
		return failed(action.Type, "Kubernetes pod lookup failed")
	}
	if !ownedReference(p.Labels, env.Identity) {
		return deny("unmanaged_resource", "only reference workload pods may be restarted")
	}
	if dryRun {
		return ActionResult{ActionType: action.Type, Authorized: true, Status: "allowed"}
	}
	if ctx.Err() != nil {
		return failed(action.Type, "action cancelled")
	}
	grace := int64(30)
	uid, rv := p.UID, p.ResourceVersion
	if err = e.client.CoreV1().Pods(env.Environment.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{GracePeriodSeconds: &grace, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}}); err != nil {
		return failed(action.Type, "Kubernetes pod deletion failed")
	}
	return ActionResult{ActionType: action.Type, Authorized: true, Executed: true, Status: "executed", Summary: "reference workload pod restart requested"}
}

func ownedReference(labels map[string]string, id kubeenv.RunIdentity) bool {
	return labels[kubeenv.ManagedLabelKey] == "true" && labels[kubeenv.RunIDLabelKey] == id.RunID && labels[kubeenv.BatchIDLabelKey] == id.BatchID && labels[kubeenv.WorkspaceIDLabelKey] == id.WorkspaceID && labels[scenariorunner.ComponentLabelKey] == scenariorunner.ReferenceComponent
}
func replicaValue(v any) (int32, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int:
		n = float64(x)
	case int32:
		n = float64(x)
	case int64:
		n = float64(x)
	default:
		return 0, false
	}
	if n != math.Trunc(n) || n > math.MaxInt32 {
		return 0, false
	}
	return int32(n), true
}
func failed(kind, summary string) ActionResult {
	return ActionResult{Code: "action_execution_failed", ActionType: kind, Authorized: true, Status: "failed", Summary: summary}
}
func (e *Executor) record(ctx context.Context, runID string, result ActionResult) {
	if e.events == nil {
		return
	}
	payload, _ := json.Marshal(result)
	eventType := "action"
	if result.Status == "denied" {
		eventType = "safety"
	}
	_, _ = e.events.AppendRunEvent(ctx, evidence.AppendRunEventInput{RunID: runID, EventType: eventType, OccurredAt: metav1.Now().Time, Summary: result.Summary, Payload: payload})
}

type Observer interface {
	Observe(context.Context, observation.EnvironmentContext) (agentgateway.Observation, error)
}
type Gateway interface {
	Invoke(context.Context, agentgateway.Connection, agentgateway.Request) (agentgateway.Response, error)
}
type StepResult struct {
	Response    agentgateway.Response
	Actions     []ActionResult
	Observation agentgateway.Observation
}

func EvaluateAgentStep(ctx context.Context, env observation.EnvironmentContext, task agentgateway.AgentTask, agentVersionID string, available []string, provider Observer, gateway Gateway, connection agentgateway.Connection, executor *Executor) (StepResult, error) {
	before, err := provider.Observe(ctx, env)
	if err != nil {
		return StepResult{}, err
	}
	response, err := gateway.Invoke(ctx, connection, agentgateway.Request{RunID: env.Identity.RunID, AgentVersionID: agentVersionID, Task: task, Observation: before, AvailableActions: available})
	if err != nil {
		return StepResult{}, err
	}
	results, err := executor.Execute(ctx, env, response.Actions)
	if err != nil {
		return StepResult{}, err
	}
	after, err := provider.Observe(ctx, env)
	if err != nil {
		return StepResult{}, err
	}
	return StepResult{Response: response, Actions: results, Observation: after}, nil
}
