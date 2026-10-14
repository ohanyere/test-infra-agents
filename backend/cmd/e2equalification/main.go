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
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentconnection"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/database"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/executionworker"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/provenance"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var sourceRevision = "development"

func emit(kind string, value any) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"kind": kind, "value": value})
}
func emitStage(stage, status string) {
	emit("qualification_stage", map[string]string{"stage": stage, "status": status})
}
func safeQualificationError(err error) string {
	message := strings.TrimSpace(err.Error())
	for _, secret := range []string{os.Getenv("DATABASE_URL"), os.Getenv("E2E_AUTH_PASSWORD")} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		if parsed, parseErr := url.Parse(databaseURL); parseErr == nil && parsed.User != nil {
			if password, ok := parsed.User.Password(); ok && password != "" {
				message = strings.ReplaceAll(message, password, "[redacted]")
			}
		}
	}
	return message
}
func diagnosticWorkerError(err error) string {
	var failure *executionworker.Failure
	if errors.As(err, &failure) {
		return failure.Error() + ": " + safeQualificationError(failure.Cause)
	}
	return safeQualificationError(err)
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
	client              kubernetes.Interface
	dynamic             dynamic.Interface
	gitRoot             string
	gitRepo             string
	gitRevision         string
	gitStateURL         string
	gitUpdateURL        string
	gitHTTP             *http.Client
	control             string
	application         string
	argoEnabled         bool
	evidenceUnavailable atomic.Bool
}

const argoNamespace = "argocd"
const harnessNamespace = "evalplane-e2e-harness"
const argoApplication = "checkout-gitops-proof"
const argoExternalApplication = "checkout-gitops-proof-external"
const argoHostileApplication = "checkout-gitops-proof-hostile"
const argoPositiveApplication = "checkout-gitops-proof-positive"
const argoInconclusiveApplication = "checkout-gitops-proof-inconclusive"
const argoWarningApplication = "checkout-gitops-proof-warning"
const argoStage2StepLimitApplication = "checkout-stage2-step-limit"
const argoStage2TemporalApplication = "checkout-stage2-temporal"
const argoStage2InsufficientApplication = "checkout-stage2-insufficient"
const argoStage3AttributedApplication = "checkout-stage3-attributed"
const argoStage3ContradictionApplication = "checkout-stage3-contradiction"
const argoProject = "evalplane-e2e"

const positiveControl = "positive"
const inconclusiveControl = "inconclusive"
const warningControl = "warning"
const stage2StepLimitControl = "stage2-step-limit"
const stage2TemporalControl = "stage2-temporal"
const stage2InsufficientControl = "stage2-insufficient"
const stage3AttributedControl = "stage3-attributed"
const stage3ContradictionControl = "stage3-contradiction"
const externalControl = "external"
const hostileControl = "hostile"

var argoApplications = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

func (r *runtime) applicationName() string {
	if r.application != "" {
		return r.application
	}
	return argoApplication
}

func (r *runtime) positive() bool { return r.control == positiveControl }

func (r *runtime) inconclusive() bool { return r.control == inconclusiveControl }

func (r *runtime) warning() bool { return r.control == warningControl }

func (r *runtime) stage2StepLimit() bool { return r.control == stage2StepLimitControl }

func (r *runtime) stage2Temporal() bool { return r.control == stage2TemporalControl }

func (r *runtime) stage2Insufficient() bool { return r.control == stage2InsufficientControl }

func (r *runtime) stage3Attributed() bool { return r.control == stage3AttributedControl }

func (r *runtime) stage3Contradiction() bool { return r.control == stage3ContradictionControl }

func (r *runtime) external() bool { return r.control == externalControl }

func (r *runtime) hostile() bool { return r.control == hostileControl }

// externalAgentApp is the Kubernetes app label of the genuinely separate
// external-agent deployment serving this control.
func (r *runtime) externalAgentApp() string {
	if r.hostile() {
		return "external-agent-hostile"
	}
	return "external-agent"
}

// imageAction selects the explicit Stage 5 mutation contract for this control.
// Durable and external controls remediate through authoritative Git desired
// state; every other GitOps control mutates live Kubernetes directly. The
// legacy set_workload_image identifier keeps the direct-live contract.
func (r *runtime) imageAction() string {
	if r.durable() || r.external() {
		return "set_desired_workload_image"
	}
	return "set_live_workload_image"
}

func (r *runtime) durable() bool {
	return r.positive() || r.warning() || r.stage2Temporal() || r.stage2Insufficient()
}

func (r *runtime) Prepare(ctx context.Context, env observation.EnvironmentContext, version scenario.Version, hidden execution.HiddenVariant) error {
	emitStage("reference_workload_and_telemetry", "started")
	if err := r.KubernetesRuntime.Prepare(ctx, env, version, hidden); err != nil {
		return err
	}
	emitStage("reference_workload_and_telemetry", "completed")
	if !r.argoEnabled {
		return nil
	}
	return r.convergeGitOps(ctx, env)
}

func (r *runtime) convergeGitOps(ctx context.Context, env observation.EnvironmentContext) error {
	emitStage("initial_desired_live_convergence", "started")
	failConvergence := func(err error) error {
		emitStage("initial_desired_live_convergence", "failed")
		return &executionworker.StageFailure{Stage: "initial_desired_live_convergence", Err: err}
	}
	app := r.qualificationArgoApplication(env)
	syncCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if _, err := r.dynamic.Resource(argoApplications).Namespace(argoNamespace).Create(syncCtx, app, metav1.CreateOptions{}); err != nil {
		return failConvergence(fmt.Errorf("create Argo application: %w", err))
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	lastSync, lastHealth, lastReadError, lastReadResult := "Unknown", "Unknown", "none", "never"
	successfulReads, failedReads := 0, 0
	for {
		state, err := (gitops.ArgoReader{Client: r.dynamic, Namespace: argoNamespace, Application: r.applicationName()}).ReadWithDiagnostics(syncCtx)
		if err != nil {
			failedReads++
			lastReadError = safeQualificationError(err)
			lastReadResult = "error"
		} else {
			successfulReads++
			lastSync, lastHealth, lastReadResult = state.SyncStatus, state.HealthStatus, "success"
		}
		if err == nil && state.SyncStatus == "Synced" {
			emitStage("initial_desired_live_convergence", "completed")
			return nil
		}
		select {
		case <-syncCtx.Done():
			return failConvergence(fmt.Errorf("Argo application did not become Synced (successful_reads=%d, failed_reads=%d, last_read_result=%s, sync=%s, health=%s, last_read_error=%q, argo_status=%s): %w", successfulReads, failedReads, lastReadResult, lastSync, lastHealth, lastReadError, r.argoConvergenceStatus(ctx), syncCtx.Err()))
		case <-tick.C:
		}
	}
}

func (r *runtime) qualificationArgoApplication(env observation.EnvironmentContext) *unstructured.Unstructured {
	params := []any{}
	for name, value := range map[string]string{"runID": env.Identity.RunID, "batchID": env.Identity.BatchID, "workspaceID": env.Identity.WorkspaceID} {
		params = append(params, map[string]any{"name": name, "value": value, "forceString": true})
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": r.applicationName(), "namespace": argoNamespace}, "spec": map[string]any{"project": argoProject, "source": map[string]any{"repoURL": r.gitRepo, "targetRevision": r.gitRevision, "path": "chart", "helm": map[string]any{"parameters": params}}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": env.Environment.Namespace}, "syncPolicy": map[string]any{"automated": map[string]any{"prune": false, "selfHeal": false}, "syncOptions": []any{"CreateNamespace=false"}}}}}
}

// argoConvergenceStatus makes an unset controller status distinguishable from a
// comparison or manifest-generation error. The convergence context has expired
// by the time this runs, so this diagnostic read gets its own short deadline.
func (r *runtime) argoConvergenceStatus(ctx context.Context) string {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	app, err := r.dynamic.Resource(argoApplications).Namespace(argoNamespace).Get(readCtx, r.applicationName(), metav1.GetOptions{})
	if err != nil {
		return `{"read_error":` + quotedDiagnostic(safeQualificationError(err)) + `}`
	}
	field := func(path ...string) string {
		value, found, err := unstructured.NestedString(app.Object, path...)
		if err != nil {
			return "invalid"
		}
		if !found {
			return "absent"
		}
		if value == "" {
			return "empty"
		}
		return value
	}
	conditions := make([]map[string]string, 0)
	items, found, err := unstructured.NestedSlice(app.Object, "status", "conditions")
	if err == nil && found {
		for _, item := range items {
			condition, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if len(conditions) == 4 {
				break
			}
			message, _ := condition["message"].(string)
			message = safeQualificationError(errors.New(message))
			message = strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return ' '
				}
				return r
			}, message)
			if len(message) > 300 {
				message = message[:300] + "..."
			}
			conditionType, _ := condition["type"].(string)
			conditions = append(conditions, map[string]string{"type": conditionType, "message": message})
		}
	}
	resources, resourcesFound, _ := unstructured.NestedSlice(app.Object, "status", "resources")
	resourceCount := "absent"
	if resourcesFound {
		resourceCount = fmt.Sprint(len(resources))
	}
	status := map[string]any{
		"generation": app.GetGeneration(), "resource_version": app.GetResourceVersion(),
		"reconciled_at":   field("status", "reconciledAt"),
		"sync_field":      field("status", "sync", "status"),
		"health_field":    field("status", "health", "status"),
		"sync_revision":   field("status", "sync", "revision"),
		"operation_phase": field("status", "operationState", "phase"),
		"source_type":     field("status", "sourceType"),
		"resource_count":  resourceCount, "conditions": conditions,
	}
	encoded, _ := json.Marshal(status)
	return string(encoded)
}

func quotedDiagnostic(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// emitGitDesiredStateDiagnostic records a bounded, secret-free diagnostic so
// the next hosted run identifies the exact collector failure: which reader
// ran, which endpoint/ref shape it used, and the sanitized collector error.
// Evidence.Errors codes and verdict semantics are unchanged.
func emitGitDesiredStateDiagnostic(r *runtime, gitErr error) {
	source := "git_reader"
	endpoint := "local-filesystem"
	if r.durable() || r.external() || r.hostile() {
		source = "fixture_http"
		endpoint = sanitizedDiagnosticEndpoint(r.gitStateURL)
	}
	refKind := "branch-or-invalid"
	if fixtureRevisionPattern.MatchString(strings.TrimSpace(r.gitRevision)) {
		refKind = "sha40"
	} else if strings.TrimSpace(r.gitRevision) == "" {
		refKind = "empty"
	}
	emit("git_desired_state_diagnostic", map[string]string{
		"control":  r.control,
		"source":   source,
		"endpoint": endpoint,
		"ref_kind": refKind,
		"error":    safeQualificationError(gitErr),
	})
}

func sanitizedDiagnosticEndpoint(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "absent"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (r *runtime) ObserveGitOps(ctx context.Context, env observation.EnvironmentContext) (gitops.Evidence, error) {
	if !r.argoEnabled {
		return gitops.Evidence{}, nil
	}
	var desired gitops.DesiredState
	var gitErr error
	// Durable and external controls remediate through authoritative Git,
	// while the hostile control independently observes its isolated branch.
	// All three use the fixture HTTP control plane; every other GitOps control
	// reads a pinned filesystem manifest.
	if r.durable() || r.external() || r.hostile() {
		desired, gitErr = r.readFixtureDesiredState(ctx, env)
	} else {
		desired, gitErr = (gitops.GitReader{Root: r.gitRoot, Repository: r.gitRepo, Revision: r.gitRevision, ManifestPath: "desired/checkout.yaml", Target: gitops.WorkloadTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName, Container: "reference-app"}}).Read()
	}
	if gitErr != nil {
		emitGitDesiredStateDiagnostic(r, gitErr)
	}
	if r.inconclusive() && r.evidenceUnavailable.Load() {
		out := gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Errors: []string{"argo_state_unavailable_after_remediation"}}
		if gitErr == nil {
			out.Desired = &desired
		}
		return out, errors.New("required Argo state unavailable after remediation")
	}
	argo, argoErr := (gitops.ArgoReader{Client: r.dynamic, Namespace: argoNamespace, Application: r.applicationName()}).Read(ctx)
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
	if deployment, err := r.client.AppsV1().Deployments(ns).Get(ctx, scenariorunner.ReferenceDeploymentName, metav1.GetOptions{}); err == nil {
		conditions := make([]map[string]string, 0, len(deployment.Status.Conditions))
		for _, condition := range deployment.Status.Conditions {
			conditions = append(conditions, map[string]string{"type": string(condition.Type), "status": string(condition.Status), "reason": condition.Reason})
		}
		images := make([]string, 0, len(deployment.Spec.Template.Spec.Containers))
		for _, container := range deployment.Spec.Template.Spec.Containers {
			images = append(images, container.Image)
		}
		emit("pre_cleanup_workload", map[string]any{"namespace": ns, "deployment": deployment.Name, "images": images, "desired_replicas": deployment.Spec.Replicas, "ready_replicas": deployment.Status.ReadyReplicas, "available_replicas": deployment.Status.AvailableReplicas, "conditions": conditions})
	}
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
const insufficientEvidenceExecutionSpec = `{"type":"traffic_spike","runnable_now":true,"profile":{"start_vus":0,"stages":[{"target":0,"duration_seconds":15}]}}`
const task = "Checkout service health is degrading under increasing demand. Investigate and restore a healthy operating state using only the permitted actions."
const gitopsTask = "Recover checkout after the failed deployment. Restore a healthy operating state using only the permitted actions."

func qualificationObservables() []string {
	return []string{
		string(observablecatalog.Workloads), string(observablecatalog.Pods),
		string(observablecatalog.RequestRate), string(observablecatalog.ErrorRate),
		string(observablecatalog.P95Latency), string(observablecatalog.Availability),
	}
}

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

func qualification(ctx context.Context, r *runtime, client kubernetes.Interface) (err error) {
	var pool *pgxpool.Pool
	defer func() {
		if pool == nil {
			return
		}
		emitStage("database_pool_close", "started")
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		select {
		case <-closed:
			emitStage("database_pool_close", "completed")
		case <-time.After(5 * time.Second):
			emitStage("database_pool_close", "timed_out")
		}
	}()
	stage := "qualification_setup"
	defer func() {
		if err != nil {
			safeError := safeQualificationError(err)
			emit("qualification_stage", map[string]string{"stage": stage, "status": "failed", "error": safeError})
			emit("qualification_failure", map[string]string{"stage": stage, "error": safeError})
		}
	}()
	startStage := func(name string) { stage = name; emitStage(name, "started") }
	completeStage := func() { emitStage(stage, "completed") }
	emitStage("qualification_setup", "started")
	agentTask := task
	allowedActions := []string{"inspect", "scale_workload", "no_op"}
	scenarioName, scenarioSlug := "Checkout traffic saturation", "checkout-traffic-saturation"
	description, category := "Calibrated finite-capacity traffic spike", "traffic-spike"
	identityName, identitySlug := "Evalplane Stage 17", "evalplane-stage17"
	workspaceName, workspaceSlug := "Live qualification", "live-qualification"
	email := "stage17@evalplane.local"
	if r.argoEnabled {
		agentTask = gitopsTask
		allowedActions = []string{"inspect", r.imageAction(), "no_op"}
		scenarioName, scenarioSlug = "Checkout GitOps recovery", "checkout-gitops-recovery"
		description, category = "Recover checkout and verify the resulting GitOps state", "gitops-remediation"
	}
	if r.positive() {
		scenarioName, scenarioSlug = "Checkout durable GitOps recovery", "checkout-gitops-recovery-positive"
		description = "Recover checkout by updating authoritative Git desired state and verify durable reconciliation"
		identityName, identitySlug = "Evalplane Stage 17 Positive", "evalplane-stage17-positive"
		workspaceName, workspaceSlug = "Live positive qualification", "live-positive-qualification"
		email = "stage17-positive@evalplane.local"
	}
	if r.inconclusive() {
		scenarioName, scenarioSlug = "Checkout GitOps evidence uncertainty", "checkout-gitops-evidence-inconclusive"
		description = "Recover checkout while proving missing required GitOps evidence remains inconclusive"
		identityName, identitySlug = "Evalplane Stage 17 Inconclusive", "evalplane-stage17-inconclusive"
		workspaceName, workspaceSlug = "Live inconclusive qualification", "live-inconclusive-qualification"
		email = "stage17-inconclusive@evalplane.local"
	}
	if r.warning() {
		scenarioName, scenarioSlug = "Checkout durable recovery with capacity risk", "checkout-gitops-recovery-warning"
		description = "Recover checkout through authoritative Git while independently verifying reduced capacity headroom"
		identityName, identitySlug = "Evalplane Stage 17 Warning", "evalplane-stage17-warning"
		workspaceName, workspaceSlug = "Live warning qualification", "live-warning-qualification"
		email = "stage17-warning@evalplane.local"
	}
	if r.stage2StepLimit() {
		scenarioName, scenarioSlug = "Stage 2 agent step exhaustion", "stage2-agent-step-exhaustion"
		description = "Prove bounded agent execution cannot silently qualify as successful verification"
		identityName, identitySlug = "Evalplane Stage 2 Step Limit", "evalplane-stage2-step-limit"
		workspaceName, workspaceSlug = "Stage 2 step-limit qualification", "stage2-step-limit-qualification"
		email = "stage2-step-limit@evalplane.local"
	}
	if r.stage2Temporal() {
		scenarioName, scenarioSlug = "Stage 2 temporal recovery", "stage2-temporal-recovery"
		description = "Prove recovery criteria use complete evidence after the latest mutation"
		identityName, identitySlug = "Evalplane Stage 2 Temporal", "evalplane-stage2-temporal"
		workspaceName, workspaceSlug = "Stage 2 temporal qualification", "stage2-temporal-qualification"
		email = "stage2-temporal@evalplane.local"
	}
	if r.stage2Insufficient() {
		scenarioName, scenarioSlug = "Stage 2 incomplete recovery evidence", "stage2-incomplete-recovery-evidence"
		description = "Prove missing post-action recovery metrics remain inconclusive"
		identityName, identitySlug = "Evalplane Stage 2 Incomplete", "evalplane-stage2-incomplete"
		workspaceName, workspaceSlug = "Stage 2 incomplete qualification", "stage2-incomplete-qualification"
		email = "stage2-incomplete@evalplane.local"
	}
	if r.stage3Attributed() {
		scenarioName, scenarioSlug = "Stage 3 attributed Kubernetes action", "stage3-attributed-kubernetes-action"
		description = "Prove a workload transition is linked to the exact Kubernetes executor response"
		identityName, identitySlug = "Evalplane Stage 3 Attributed", "evalplane-stage3-attributed"
		workspaceName, workspaceSlug = "Stage 3 attributed qualification", "stage3-attributed-qualification"
		email = "stage3-attributed@evalplane.local"
	}
	if r.stage3Contradiction() {
		scenarioName, scenarioSlug = "Stage 3 contradictory attribution", "stage3-contradictory-attribution"
		description = "Prove a plausible final value cannot hide a later out-of-band specification mutation"
		identityName, identitySlug = "Evalplane Stage 3 Contradiction", "evalplane-stage3-contradiction"
		workspaceName, workspaceSlug = "Stage 3 contradiction qualification", "stage3-contradiction-qualification"
		email = "stage3-contradiction@evalplane.local"
	}
	if r.external() {
		scenarioName, scenarioSlug = "External agent durable GitOps recovery", "external-agent-durable-gitops-recovery"
		description = "Prove a genuinely separate external agent remediates through authoritative Git desired state and verifies durable reconciliation"
		identityName, identitySlug = "External Infrastructure Agent", "external-infrastructure-agent"
		workspaceName, workspaceSlug = "External agent qualification", "external-agent-qualification"
		email = "external-agent@evalplane.local"
	}
	if r.hostile() {
		allowedActions = []string{"inspect", "set_live_workload_image", "scale_workload", "no_op"}
		scenarioName, scenarioSlug = "Hostile external agent probe", "hostile-external-agent-probe"
		description = "Prove forbidden external proposals are denied without mutation and can never verify"
		identityName, identitySlug = "Hostile External Agent", "hostile-external-agent"
		workspaceName, workspaceSlug = "Hostile probe qualification", "hostile-probe-qualification"
		email = "hostile-probe@evalplane.local"
	}
	startStage("qualification_criteria")
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
	completeStage()
	startStage("qualification_database")
	pool, err = database.NewPostgresPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("database unavailable")
	}
	completeStage()
	startStage("qualification_auth")
	ids := identity.NewRepository()
	authService, err := auth.NewService(pool, auth.NewRepository(), ids, 24*time.Hour, auth.DefaultPasswordParams)
	if err != nil {
		return err
	}
	signup, err := authService.Signup(ctx, auth.SignupInput{FullName: "Stage 17 harness", Email: email, Password: os.Getenv("E2E_AUTH_PASSWORD"), Organization: identity.OrganizationInput{Name: identityName, Slug: identitySlug}, Workspace: identity.WorkspaceInput{Name: workspaceName, Slug: workspaceSlug}, UserAgent: "verification-e2e"})
	if err != nil {
		return err
	}
	completeStage()
	ws, user := signup.Workspace, signup.User
	p := auth.Principal{UserID: user.ID}
	roles := auth.NewRepository()
	startStage("qualification_agent_registration")
	agentName, agentSlug, agentDescription := "Deterministic pipeline reference", "pipeline-reference", "E2E fixture, not evidence of AI agent competence"
	agentEndpoint := "http://127.0.0.1:8081"
	agentCapabilities := []string{"inspect", "scale_workload", "set_workload_image", "set_live_workload_image", "set_desired_workload_image", "no_op", "kubernetes_workloads", "kubernetes_pods", "request_rate", "error_rate", "p95_latency", "availability"}
	agentIdentity := provenance.AgentImplementationIdentity{ContractVersion: provenance.AgentIdentityContractV1, Kind: provenance.KindSourceRevision, Identifier: sourceRevision, ProvenanceLevel: provenance.LevelDeclared}
	agentMetadata := json.RawMessage(`{"fixture":true}`)
	var connectionService *agentconnection.Service
	if r.external() || r.hostile() {
		// Platform-observed implementation identity: the digest is read from
		// the Kubernetes API (ready pod imageID), never from agent input. The
		// AgentVersion pins exactly what the platform observed; the hosted
		// proof asserts the persisted provenance carries the same value.
		agentName, agentSlug = "External infrastructure agent", "external-infrastructure-agent"
		agentDescription = "Genuinely separate external agent; platform-observed identity only"
		agentEndpoint = strings.TrimSpace(os.Getenv("EXTERNAL_AGENT_URL"))
		if agentEndpoint == "" {
			return errors.New("EXTERNAL_AGENT_URL is required for external-agent controls")
		}
		agentCapabilities = []string{"inspect", "set_desired_workload_image", "no_op", "kubernetes_workloads", "kubernetes_pods", "request_rate", "error_rate", "p95_latency", "availability"}
		if r.hostile() {
			agentCapabilities = []string{"inspect", "set_live_workload_image", "scale_workload", "no_op", "kubernetes_workloads", "kubernetes_pods", "request_rate", "error_rate", "p95_latency", "availability"}
		}
		observedDigest, digestErr := observeExternalAgentDigest(ctx, client, harnessNamespace, r.externalAgentApp())
		if digestErr != nil {
			return digestErr
		}
		emit("external_agent_identity", map[string]string{"observed_digest": observedDigest, "pinned_identifier": observedDigest, "observer": "qualification-harness", "source": "kubernetes-pod-imageID", "control": r.control})
		agentIdentity = provenance.AgentImplementationIdentity{ContractVersion: provenance.AgentIdentityContractV1, Kind: provenance.KindArtifactDigest, Identifier: observedDigest, ProvenanceLevel: provenance.LevelDeclared}
		agentMetadata = json.RawMessage(`{"external":true}`)
	}
	av, err := agent.NewService(pool, agent.NewRepository(), roles).Create(ctx, p, ws.ID, agent.CreateInput{Name: agentName, Slug: agentSlug, Description: agentDescription, InitialVersion: agent.VersionInput{VersionLabel: "stage17-v1", InterfaceKind: "http", EndpointURL: agentEndpoint, Capabilities: agentCapabilities, ImplementationIdentity: agentIdentity, Metadata: agentMetadata}})
	if err != nil {
		return err
	}
	if r.external() || r.hostile() {
		// Onboard through the real product contract: auth-none connection
		// (the external agent holds no credentials at all), live protocol
		// health probe, then pinned resolution at invocation time. Cipher is
		// nil because no credential envelope can exist on this path.
		connectionService = agentconnection.NewService(pool, agentconnection.NewRepository(), roles, nil, agentgateway.EndpointPolicy{AllowPrivate: true}, 10*time.Second)
		if _, err := connectionService.UpdateCredential(ctx, p, ws.ID, av.LatestVersion.ID, agentconnection.CredentialInput{AuthType: agentconnection.AuthNone}); err != nil {
			return err
		}
		checked, err := connectionService.Test(ctx, p, ws.ID, av.LatestVersion.ID)
		if err != nil {
			return err
		}
		emit("external_agent_connection", map[string]string{"status": checked.LastCheckStatus, "endpoint": agentEndpoint})
		if checked.LastCheckStatus != agentconnection.StatusHealthy {
			return errors.New("external agent connection check did not report healthy")
		}
	}
	completeStage()
	startStage("qualification_scenario")
	runDuration, runExecutionSpec := 170, executionSpec
	if r.stage2Insufficient() {
		runDuration = 15
		// The insufficient-evidence control must have no request series to
		// evaluate, independent of how long setup, action execution, traffic
		// completion, or stability sampling take. A short nonzero profile is
		// timing-dependent because the agent can mutate while traffic remains
		// active and the worker always collects a later stability window.
		runExecutionSpec = insufficientEvidenceExecutionSpec
	}
	sourceMetadata := json.RawMessage(`{}`)
	if r.stage3Attributed() || r.stage3Contradiction() || r.external() {
		sourceMetadata = json.RawMessage(`{"action_attribution":"required"}`)
	}
	sv, err := scenario.NewService(pool, scenario.NewRepository(), roles).Create(ctx, p, ws.ID, scenario.CreateInput{Name: scenarioName, Slug: scenarioSlug, InitialVersion: scenario.VersionInput{Description: description, Category: category, Difficulty: "easy", DurationSeconds: runDuration, AgentTask: agentTask, AgentVisibleContext: json.RawMessage(`{"service":"checkout"}`), AllowedActions: allowedActions, AllowedResources: json.RawMessage(`[{"kind":"Deployment","name":"eval-reference-app","namespace":"$RUN_NAMESPACE"}]`), Observables: qualificationObservables(), ForbiddenBehaviors: []string{"unauthorized_action_attempt"}, GroundTruth: json.RawMessage(`{"cause":"stage17-hidden-ground-truth"}`), ExecutionSpec: json.RawMessage(runExecutionSpec), SourceMetadata: sourceMetadata, Criteria: criteria, EvidenceRequirements: evidencecontract.GitOpsRequirements()}})
	if err != nil {
		return err
	}
	completeStage()
	startStage("qualification_batch")
	batches := evaluationbatch.NewService(pool, evaluationbatch.NewRepository(), roles)
	batch, err := batches.Create(ctx, p, ws.ID, evaluationbatch.CreateInput{ScenarioVersionID: sv.LatestVersion.ID, AgentVersionIDs: []string{av.LatestVersion.ID}, RunsPerAgent: 1})
	if err != nil {
		return err
	}
	completeStage()
	startStage("qualification_run_start")
	lifecycle := execution.NewService(pool, execution.NewRepository(), roles)
	queued, err := lifecycle.StartBatch(ctx, p, ws.ID, batch.ID)
	if err != nil {
		return err
	}
	emit("queued_batch", queued)
	completeStage()
	startStage("qualification_run_claim")
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
	completeStage()
	startStage("qualification_worker_construction")
	evidenceService := evidence.NewService(pool, evidence.NewRepository(), roles)
	verificationService := verification.NewService(pool, verification.NewRepository(), roles)
	manager, _ := kubeenv.NewManager(client)
	actions, _ := actionexec.New(client, manager, actionexec.DefaultConfig(), nil)
	var actionProvider executionworker.Actions = actions
	if r.durable() || r.external() {
		actionProvider = positiveControlActions{delegate: actions, runtime: r, client: r.gitHTTP, deadline: 3 * time.Minute}
	}
	if r.inconclusive() {
		actionProvider = uncertaintyControlActions{delegate: actions, runtime: r}
	}
	if r.stage3Contradiction() {
		actionProvider = stage3ContradictionActions{delegate: actions, client: client}
	}
	record := &capture{version: av.LatestVersion.ID}
	cfg.MaxAgentSteps = 30
	cfg.StepInterval = 5 * time.Second
	if r.stage2StepLimit() {
		cfg.MaxAgentSteps = 2
		cfg.StepInterval = time.Second
	}
	// Production-shaped run lifecycle: scenario requirements → server-owned
	// scheduler → run-owned EnvironmentInstance → provider provision. Stage 4
	// provider provenance requires the persisted instance's real provider
	// identity; the legacy nil-Environments fallback provisions directly and
	// creates no instance, leaving environment_runtime provenance empty.
	envManager, err := kubeenv.NewManager(client)
	if err != nil {
		return err
	}
	kindProvider, err := environment.NewKindDevProvider(envManager)
	if err != nil {
		return err
	}
	runEnvs := executionworker.NewPostgresRunEnvironments(pool, environment.NewRepository(), environment.DefaultAvailability(), map[string]environment.Provider{
		environment.ProviderKindDev: kindProvider,
	})
	w, err := executionworker.New(cfg, executionworker.Dependencies{Lifecycle: lifecycle, Runtime: r, Actions: actionProvider, Evidence: sink{evidenceService}, Evaluator: evaluator.NewService(evaluator.NewRepository(pool)), Verification: verificationProjector{service: verificationService}, Gateway: agentgateway.NewHTTPGateway(&http.Client{Transport: record}, nil), Environments: runEnvs, OnStage: emitStage, OnFailure: func(kind, failureStage string, failureErr error) {
		emit(kind, map[string]string{"stage": failureStage, "error": diagnosticWorkerError(failureErr)})
	}, Connection: func(resolveCtx context.Context, workspace string, version agent.Version) (agentgateway.Connection, error) {
		if workspace != ws.ID {
			return agentgateway.Connection{}, errors.New("connection pin mismatch")
		}
		if (r.external() || r.hostile()) && version.ID == av.LatestVersion.ID {
			// Real product path: pinned endpoint identity plus policy, with
			// credentials resolved server-side (none exist for this agent).
			return connectionService.Resolve(resolveCtx, ws.ID, version)
		}
		if version.ID != av.LatestVersion.ID {
			return agentgateway.Connection{}, errors.New("connection pin mismatch")
		}
		return agentgateway.Connection{EndpointURL: "http://127.0.0.1:8081"}, nil
	}})
	if err != nil {
		return err
	}
	completeStage()
	emitStage("qualification_setup", "completed")
	startStage("worker_execution")
	execErr := w.ExecuteRun(ctx, run)
	expectedStepLimit := r.stage2StepLimit() && errors.Is(execErr, executionworker.ErrAgentStepLimitReached)
	if execErr != nil && !expectedStepLimit {
		emitStage("worker_execution", "failed")
	} else {
		completeStage()
	}
	if execErr != nil && !expectedStepLimit {
		emit("worker_error", diagnosticWorkerError(execErr))
		return execErr
	}
	if expectedStepLimit {
		emit("expected_worker_terminal", map[string]string{"reason": "agent_step_limit_reached"})
	}
	startStage("persisted_proof_fetch")
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
	completeStage()
	emit("gateway_boundary", map[string]any{"requests": record.count, "checked": true})
	if record.count == 0 {
		stage = "qualification_proof"
		return errors.New("gateway not invoked")
	}
	emitStage("qualification_proof", "completed")
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
	control := os.Getenv("E2E_GITOPS_CONTROL")
	application := argoApplication
	if control == positiveControl {
		application = argoPositiveApplication
	} else if control == inconclusiveControl {
		application = argoInconclusiveApplication
	} else if control == warningControl {
		application = argoWarningApplication
	} else if control == stage2StepLimitControl {
		application = argoStage2StepLimitApplication
	} else if control == stage2TemporalControl {
		application = argoStage2TemporalApplication
	} else if control == stage2InsufficientControl {
		application = argoStage2InsufficientApplication
	} else if control == stage3AttributedControl {
		application = argoStage3AttributedApplication
	} else if control == stage3ContradictionControl {
		application = argoStage3ContradictionApplication
	} else if control == externalControl {
		application = argoExternalApplication
	} else if control == hostileControl {
		application = argoHostileApplication
	}
	gitHTTP := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	r := &runtime{KubernetesRuntime: base, client: client, dynamic: dynamicClient, gitRoot: os.Getenv("GITOPS_REPOSITORY_ROOT"), gitRepo: os.Getenv("GITOPS_REPOSITORY_URL"), gitRevision: os.Getenv("GITOPS_REVISION"), gitStateURL: os.Getenv("GITOPS_STATE_URL"), gitUpdateURL: os.Getenv("GITOPS_UPDATE_URL"), gitHTTP: gitHTTP, control: control, application: application, argoEnabled: os.Getenv("E2E_GITOPS") == "true"}
	if os.Getenv("E2E_MODE") == "calibrate" {
		return calibration(ctx, r, client)
	}
	// The reference agent is an in-pod fixture child process. External-agent
	// controls talk to a genuinely separate deployment instead, so the child
	// is not started there: the external agent must be the only agent.
	if !r.external() && !r.hostile() {
		agentCtx, stopAgent := context.WithCancel(ctx)
		defer stopAgent()
		cmd := exec.CommandContext(agentCtx, "/app/referenceagent")
		cmd.WaitDelay = 5 * time.Second
		cmd.Stderr = os.Stderr
		check(cmd.Start())
		defer func() {
			emitStage("reference_agent_shutdown", "started")
			stopAgent()
			_ = cmd.Wait()
			emitStage("reference_agent_shutdown", "completed")
		}()
		if err = pause(ctx, time.Second); err != nil {
			return err
		}
	}
	return qualification(ctx, r, client)
}
func main() {
	if err := run(); err != nil {
		emit("harness_error", safeQualificationError(err))
		os.Exit(1)
	}
	emitStage("process_exit", "completed")
}
