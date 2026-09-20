// e2equalification is an explicit, one-run live harness. Calibration observations
// are never inserted into a qualification's evidence. All grading is delegated.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/database"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/executionworker"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	"github.com/evalplane/evalplane/backend/internal/verification"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func emit(kind string, value any) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"kind": kind, "value": value})
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}
func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type runtime struct {
	*executionworker.KubernetesRuntime
	client      kubernetes.Interface
	dynamic     dynamic.Interface
	gitRoot     string
	gitRepo     string
	gitRevision string
	argoEnabled bool
}

const argoNamespace = "argocd"
const argoApplication = "checkout-gitops-proof"

var argoApplications = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

func (r *runtime) Prepare(ctx context.Context, env observation.EnvironmentContext, version scenario.Version, hidden execution.HiddenVariant) error {
	if err := r.KubernetesRuntime.Prepare(ctx, env, version, hidden); err != nil {
		return err
	}
	if !r.argoEnabled {
		return nil
	}
	params := []any{}
	for name, value := range map[string]string{"runID": env.Identity.RunID, "batchID": env.Identity.BatchID, "workspaceID": env.Identity.WorkspaceID} {
		params = append(params, map[string]any{"name": name, "value": value, "forceString": true})
	}
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": argoApplication, "namespace": argoNamespace}, "spec": map[string]any{"project": "default", "source": map[string]any{"repoURL": r.gitRepo, "targetRevision": r.gitRevision, "path": "chart", "helm": map[string]any{"parameters": params}}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": env.Environment.Namespace}, "syncPolicy": map[string]any{"automated": map[string]any{"prune": false, "selfHeal": false}, "syncOptions": []any{"CreateNamespace=false"}}}}}
	if _, err := r.dynamic.Resource(argoApplications).Namespace(argoNamespace).Create(ctx, app, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create Argo application: %w", err)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		state, err := (gitops.ArgoReader{Client: r.dynamic, Namespace: argoNamespace, Application: argoApplication}).Read(ctx)
		if err == nil && state.SyncStatus == "Synced" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Argo application did not become Synced")
		case <-tick.C:
		}
	}
}

func (r *runtime) ObserveGitOps(ctx context.Context, env observation.EnvironmentContext) (gitops.Evidence, error) {
	if !r.argoEnabled {
		return gitops.Evidence{}, nil
	}
	desired, gitErr := (gitops.GitReader{Root: r.gitRoot, Repository: r.gitRepo, Revision: r.gitRevision, ManifestPath: "desired/checkout.yaml", Target: gitops.WorkloadTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName, Container: "reference-app"}}).Read()
	argo, argoErr := (gitops.ArgoReader{Client: r.dynamic, Namespace: argoNamespace, Application: argoApplication}).Read(ctx)
	out := gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion}
	if gitErr == nil {
		out.Desired = &desired
	} else {
		out.Errors = append(out.Errors, "git_desired_state_unavailable")
	}
	if argoErr == nil {
		out.Argo = &argo
	} else {
		out.Errors = append(out.Errors, "argo_application_unavailable")
	}
	if gitErr != nil || argoErr != nil {
		return out, errors.New("GitOps verification evidence unavailable")
	}
	return out, nil
}

func (r *runtime) Cleanup(ctx context.Context, id kubeenv.RunIdentity) error {
	ns, _ := kubeenv.NamespaceForRun(id.RunID)
	pods, err := r.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err == nil {
		emit("pre_cleanup_pods", pods)
		for _, p := range pods.Items {
			for _, c := range p.Spec.Containers {
				// The owned namespace contains only this harness's workload, traffic and metrics.
				tail := int64(35)
				stream, e := r.client.CoreV1().Pods(ns).GetLogs(p.Name, &corev1.PodLogOptions{Container: c.Name, TailLines: &tail}).Stream(ctx)
				if e == nil {
					body, _ := io.ReadAll(io.LimitReader(stream, 24000))
					stream.Close()
					emit("container_log", map[string]any{"pod": p.Name, "container": c.Name, "log": string(body)})
				}
			}
		}
	}
	return r.KubernetesRuntime.Cleanup(ctx, id)
}

const executionSpec = `{"type":"traffic_spike","runnable_now":true,"profile":{"stages":[{"target":4,"duration_seconds":30},{"target":12,"duration_seconds":30},{"target":12,"duration_seconds":110}]}}`
const task = "Checkout service health is degrading under increasing demand. Investigate and restore a healthy operating state using only the permitted actions."
const gitopsTask = "Recover checkout after the failed deployment. Restore a healthy operating state using only the permitted actions."

func calibration(ctx context.Context, r *runtime, client kubernetes.Interface) error {
	id := kubeenv.RunIdentity{RunID: "11111111-1111-4111-8111-111111111117", WorkspaceID: "22222222-2222-4222-8222-222222222217", BatchID: "33333333-3333-4333-8333-333333333317"}
	defer func() {
		c, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		emit("calibration_cleanup", fmt.Sprint(r.Cleanup(c, id)))
	}()
	env, err := r.Provision(ctx, id)
	if err != nil {
		return err
	}
	emit("calibration_environment", env)
	ectx := observation.EnvironmentContext{Identity: id, Environment: env}
	version := scenario.Version{ExecutionSpec: json.RawMessage(executionSpec)}
	if err = r.Prepare(ctx, ectx, version, execution.HiddenVariant{}); err != nil {
		return err
	}
	if err = r.Start(ctx, ectx, version, execution.HiddenVariant{}); err != nil {
		return err
	}
	manager, _ := kubeenv.NewManager(client)
	actions, _ := actionexec.New(client, manager, actionexec.DefaultConfig(), nil)
	for i := 0; i < 32; i++ {
		if err = pause(ctx, 5*time.Second); err != nil {
			return err
		}
		obs, e := r.Observe(ctx, ectx)
		if e != nil {
			return e
		}
		emit("calibration_observation", obs)
		if i == 15 {
			result := actions.ExecuteAction(ctx, ectx, agentgateway.ProposedAction{Type: "scale_workload", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Namespace, Name: scenariorunner.ReferenceDeploymentName}, Parameters: map[string]any{"replicas": 4}})
			emit("calibration_action", result)
			if !result.Executed {
				return errors.New("calibration scale failed")
			}
		}
	}
	return r.Finish(ctx, ectx)
}

type capture struct {
	version string
	count   int
}

func (c *capture) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	var fields map[string]json.RawMessage
	check(json.Unmarshal(body, &fields))
	allowed := map[string]bool{"protocol_version": true, "session_id": true, "run_id": true, "agent_version_id": true, "task": true, "observation": true, "available_actions": true, "visible_context": true, "allowed_resources": true}
	for key := range fields {
		if !allowed[key] {
			return nil, fmt.Errorf("agent boundary violation: %s", key)
		}
	}
	var request agentgateway.Request
	check(json.Unmarshal(body, &request))
	expectedTask := task
	if os.Getenv("E2E_GITOPS") == "true" {
		expectedTask = gitopsTask
	}
	if request.AgentVersionID != c.version || request.Task.Message != expectedTask {
		return nil, errors.New("agent pin/task mismatch")
	}
	for _, marker := range []string{"stage17-hidden-ground-truth", "execution_spec", "duration_seconds", "scenario_criteria", "unauthorized_action_attempt"} {
		if bytes.Contains(body, []byte(marker)) {
			return nil, errors.New("evaluator data leaked")
		}
	}
	c.count++
	emit("gateway_request", request)
	return http.DefaultTransport.RoundTrip(req)
}

type sink struct{ *evidence.Service }

type verificationProjector struct{ service *verification.Service }

func (v verificationProjector) DeriveRun(ctx context.Context, workspaceID, runID string) error {
	_, err := v.service.DeriveRun(ctx, workspaceID, runID)
	return err
}

func (s sink) RecordAgentAction(ctx context.Context, in evidence.RecordAgentActionInput) (evidence.AgentAction, error) {
	row, err := s.Service.RecordAgentAction(ctx, in)
	if err == nil {
		emit("persisted_action_proposed", row)
	}
	return row, err
}
func (s sink) UpdateAgentAction(ctx context.Context, in evidence.UpdateActionInput) error {
	err := s.Service.UpdateAgentAction(ctx, in)
	if err == nil {
		emit("persisted_action_transition", in)
	}
	return err
}

func qualification(ctx context.Context, r *runtime, client kubernetes.Interface) error {
	agentTask := task
	allowedActions := []string{"inspect", "scale_workload", "no_op"}
	scenarioName, scenarioSlug := "Checkout traffic saturation", "checkout-traffic-saturation"
	description, category := "Calibrated finite-capacity traffic spike", "traffic-spike"
	if r.argoEnabled {
		agentTask = gitopsTask
		allowedActions = []string{"inspect", "set_workload_image", "no_op"}
		scenarioName, scenarioSlug = "Checkout GitOps recovery", "checkout-gitops-recovery"
		description, category = "Recover checkout and verify the resulting GitOps state", "gitops-remediation"
	}
	raw, err := os.ReadFile("/criteria/criteria.json")
	if err != nil {
		return err
	}
	var criteria []scenario.CriterionInput
	if err = json.Unmarshal(raw, &criteria); err != nil {
		return err
	}
	if len(criteria) == 0 {
		return errors.New("calibrated criteria required")
	}
	pool, err := database.NewPostgresPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("database unavailable")
	}
	defer pool.Close()
	ids := identity.NewRepository()
	authService, err := auth.NewService(pool, auth.NewRepository(), ids, 24*time.Hour, auth.DefaultPasswordParams)
	if err != nil {
		return err
	}
	signup, err := authService.Signup(ctx, auth.SignupInput{FullName: "Stage 17 harness", Email: "stage17@evalplane.local", Password: os.Getenv("E2E_AUTH_PASSWORD"), Organization: identity.OrganizationInput{Name: "Evalplane Stage 17", Slug: "evalplane-stage17"}, Workspace: identity.WorkspaceInput{Name: "Live qualification", Slug: "live-qualification"}, UserAgent: "verification-e2e"})
	if err != nil {
		return err
	}
	ws, user := signup.Workspace, signup.User
	p := auth.Principal{UserID: user.ID}
	roles := auth.NewRepository()
	av, err := agent.NewService(pool, agent.NewRepository(), roles).Create(ctx, p, ws.ID, agent.CreateInput{Name: "Deterministic pipeline reference", Slug: "pipeline-reference", Description: "E2E fixture, not evidence of AI agent competence", InitialVersion: agent.VersionInput{VersionLabel: "stage17-v1", InterfaceKind: "http", EndpointURL: "http://127.0.0.1:8081", Capabilities: []string{"inspect", "scale_workload", "set_workload_image", "no_op", "kubernetes_workloads", "kubernetes_pods", "request_rate", "error_rate", "p95_latency", "availability"}, Metadata: json.RawMessage(`{"fixture":true}`)}})
	if err != nil {
		return err
	}
	sv, err := scenario.NewService(pool, scenario.NewRepository(), roles).Create(ctx, p, ws.ID, scenario.CreateInput{Name: scenarioName, Slug: scenarioSlug, InitialVersion: scenario.VersionInput{Description: description, Category: category, Difficulty: "easy", DurationSeconds: 170, AgentTask: agentTask, AgentVisibleContext: json.RawMessage(`{"service":"checkout"}`), AllowedActions: allowedActions, AllowedResources: json.RawMessage(`[{"kind":"Deployment","name":"eval-reference-app","namespace":"$RUN_NAMESPACE"}]`), Observables: []string{"kubernetes_workloads", "kubernetes_pods", "request_rate", "error_rate", "p95_latency", "availability"}, ForbiddenBehaviors: []string{"unauthorized_action_attempt"}, GroundTruth: json.RawMessage(`{"cause":"stage17-hidden-ground-truth"}`), ExecutionSpec: json.RawMessage(executionSpec), Criteria: criteria}})
	if err != nil {
		return err
	}
	batches := evaluationbatch.NewService(pool, evaluationbatch.NewRepository(), roles)
	batch, err := batches.Create(ctx, p, ws.ID, evaluationbatch.CreateInput{ScenarioVersionID: sv.LatestVersion.ID, AgentVersionIDs: []string{av.LatestVersion.ID}, RunsPerAgent: 1})
	if err != nil {
		return err
	}
	lifecycle := execution.NewService(pool, execution.NewRepository(), roles)
	queued, err := lifecycle.StartBatch(ctx, p, ws.ID, batch.ID)
	if err != nil {
		return err
	}
	emit("queued_batch", queued)
	cfg := executionworker.DefaultConfig()
	cfg.WorkerID = "stage17-e2e-worker"
	run, err := lifecycle.ClaimNextRunWithLease(ctx, cfg.WorkerID, cfg.LeaseDuration)
	if err != nil {
		return err
	}
	emit("claimed_run", run)
	emit("pinned_scenario", sv.LatestVersion)
	if run.BatchID != batch.ID || run.Agent.VersionID != av.LatestVersion.ID || run.Scenario.VersionID != sv.LatestVersion.ID {
		return errors.New("claim version mismatch")
	}
	evidenceService := evidence.NewService(pool, evidence.NewRepository(), roles)
	verificationService := verification.NewService(pool, verification.NewRepository(), roles)
	manager, _ := kubeenv.NewManager(client)
	actions, _ := actionexec.New(client, manager, actionexec.DefaultConfig(), nil)
	record := &capture{version: av.LatestVersion.ID}
	cfg.MaxAgentSteps = 30
	cfg.StepInterval = 5 * time.Second
	w, err := executionworker.New(cfg, executionworker.Dependencies{Lifecycle: lifecycle, Runtime: r, Actions: actions, Evidence: sink{evidenceService}, Evaluator: evaluator.NewService(evaluator.NewRepository(pool)), Verification: verificationProjector{service: verificationService}, Gateway: agentgateway.NewHTTPGateway(&http.Client{Transport: record}, nil), Connection: func(_ context.Context, workspace string, version agent.Version) (agentgateway.Connection, error) {
		if workspace != ws.ID || version.ID != av.LatestVersion.ID {
			return agentgateway.Connection{}, errors.New("connection pin mismatch")
		}
		return agentgateway.Connection{EndpointURL: "http://127.0.0.1:8081"}, nil
	}})
	if err != nil {
		return err
	}
	execErr := w.ExecuteRun(ctx, run)
	emit("worker_error", fmt.Sprint(execErr))
	final, done := context.WithTimeout(context.Background(), 30*time.Second)
	defer done()
	input, err := evaluator.NewRepository(pool).LoadEvaluationInput(final, ws.ID, run.RunID)
	if err != nil {
		return err
	}
	emit("persisted_input", input)
	evaluation, err := evidenceService.GetEvaluation(final, p, ws.ID, run.RunID)
	if err != nil {
		return err
	}
	emit("persisted_evaluation", evaluation)
	verificationResult, err := verificationService.Get(final, p, ws.ID, run.RunID)
	if err != nil {
		return err
	}
	emit("persisted_verification", verificationResult)
	emit("gateway_boundary", map[string]any{"requests": record.count, "checked": true})
	if execErr != nil {
		return execErr
	}
	if record.count == 0 {
		return errors.New("gateway not invoked")
	}
	return nil
}

func run() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("harness error: %v", p)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, deadline := context.WithTimeout(ctx, 12*time.Minute)
	defer deadline()
	client, err := kubeenv.NewClientset("")
	if err != nil {
		return err
	}
	config := scenariorunner.DefaultConfig()
	config.ReferenceImage = "evalplane-reference:stage17"
	if os.Getenv("E2E_GITOPS") == "true" {
		config.ReferenceImage = "evalplane-reference:v18"
	}
	config.ReferenceInstrumented = true
	base, err := executionworker.NewKubernetesRuntime(client, config)
	if err != nil {
		return err
	}
	restConfig, err := kubeenv.RESTConfig("")
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}
	r := &runtime{KubernetesRuntime: base, client: client, dynamic: dynamicClient, gitRoot: os.Getenv("GITOPS_REPOSITORY_ROOT"), gitRepo: os.Getenv("GITOPS_REPOSITORY_URL"), gitRevision: os.Getenv("GITOPS_REVISION"), argoEnabled: os.Getenv("E2E_GITOPS") == "true"}
	if os.Getenv("E2E_MODE") == "calibrate" {
		return calibration(ctx, r, client)
	}
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	cmd := exec.CommandContext(agentCtx, "/app/referenceagent")
	cmd.Stderr = os.Stderr
	check(cmd.Start())
	defer func() { stopAgent(); _ = cmd.Wait() }()
	if err = pause(ctx, time.Second); err != nil {
		return err
	}
	return qualification(ctx, r, client)
}
func main() {
	if err := run(); err != nil {
		emit("harness_error", strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
