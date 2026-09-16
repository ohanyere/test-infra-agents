package srequal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
)

// versionIDs pins one deterministic version identity per task. These are
// fixture identities for focused certification validation, not database rows.
func versionID(taskID string) string {
	sum := sha256.Sum256([]byte("srequal:v1:" + taskID))
	hexed := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-4%s-%s-%s",
		hexed[0:8], hexed[8:12], hexed[13:16], hexed[16:20], hexed[20:32])
}

// Version converts a task into the existing scenario model for deterministic
// grading and fake certification. AgentTask and visible context carry symptoms
// only; ground truth, reference remediation, and hidden values stay out.
func (t Task) Version() scenario.Version {
	criteria := make([]scenario.Criterion, 0, len(t.Criteria))
	for i, c := range t.Criteria {
		criteria = append(criteria, scenario.Criterion{
			ID: fmt.Sprintf("%s-c%d", t.ID, i), Key: c.Key, Name: c.Name,
			MetricKey: c.MetricKey, Operator: c.Operator, TargetValue: c.Target,
			Unit: c.Unit, SortOrder: i,
		})
	}
	visible := t.VisibleContext
	if len(visible) == 0 {
		visible = json.RawMessage(`{}`)
	}
	resources := t.AllowedResources
	if len(resources) == 0 {
		resources = json.RawMessage(`[]`)
	}
	conditions := t.Conditions
	if len(conditions) == 0 {
		conditions = json.RawMessage(`[]`)
	}
	return scenario.Version{
		ID: versionID(t.ID), Description: t.Description, Category: categoryFor(t),
		Difficulty: t.Difficulty, DurationSeconds: t.DurationSeconds(),
		Environment: json.RawMessage(`{}`), Conditions: conditions,
		Observables:        append([]string(nil), t.Observables...),
		ForbiddenBehaviors: append([]string(nil), t.ForbiddenBehaviors...),
		AuthoringMethod:    "manual", AgentTask: t.AgentTask, AgentVisibleContext: visible,
		AllowedActions:   append([]string(nil), t.AllowedActions...),
		AllowedResources: resources, GroundTruth: append(json.RawMessage(nil), t.GroundTruth...),
		ExecutionSpec: json.RawMessage(`{"type":"traffic_spike"}`), SourceMetadata: json.RawMessage(`{}`),
		Criteria: criteria,
	}
}

func categoryFor(t Task) string {
	switch {
	case strings.HasPrefix(t.Family, "kubernetes.capacity"):
		return "capacity"
	case strings.HasPrefix(t.Family, "kubernetes.deployment"):
		return "deployment"
	case strings.HasPrefix(t.Family, "kubernetes.networking"):
		return "networking"
	case strings.HasPrefix(t.Family, "kubernetes.workload"):
		return "workload"
	case strings.HasPrefix(t.Family, "observability"):
		return "observability"
	case strings.HasPrefix(t.Family, "safety"):
		return "safety"
	case strings.HasPrefix(t.Family, "delivery"):
		return "delivery"
	default:
		return "general"
	}
}

// DurationSeconds parses the display duration back to seconds for the model.
func (t Task) DurationSeconds() int {
	var minutes int
	if _, err := fmt.Sscanf(t.Duration, "%d min", &minutes); err == nil && minutes > 0 {
		return minutes * 60
	}
	return 600
}

// ReferenceInput returns the evaluator-only reference remediation input.
func (t Task) ReferenceInput() scenariocertification.ReferenceRemediationInput {
	return scenariocertification.ReferenceRemediationInput{
		Preconditions:     append(json.RawMessage(nil), t.Reference.Preconditions...),
		ReferenceActions:  append([]scenariocertification.ReferenceAction(nil), t.Reference.Actions...),
		ExpectedState:     append(json.RawMessage(nil), t.Reference.ExpectedState...),
		VerificationNotes: t.Reference.VerificationNotes,
		CleanupExpected:   t.Reference.CleanupExpected,
	}
}

// AgentPayloads serializes exactly the agent-facing surfaces: AgentView, an
// HTTP gateway-shaped invocation payload, and an MCP tools/list-shaped
// payload. Evaluator-only fields must never appear here; leakage tests assert
// that plus sentinel absence.
func (t Task) AgentPayloads() []string {
	version := t.Version()
	view := version.AgentView()
	viewJSON, _ := json.Marshal(view)
	gateway := map[string]any{
		"protocol_version":     "v1",
		"task":                 map[string]any{"message": view.Task},
		"visible_context":      view.VisibleContext,
		"available_actions":    view.AllowedActions,
		"allowed_resources":    view.AllowedResources,
		"allowed_observations": view.AllowedObservations,
	}
	gatewayJSON, _ := json.Marshal(gateway)
	mcp := map[string]any{
		"protocol_version": "2026-07-28",
		"tools": []string{
			"evalplane_observe_workloads", "evalplane_observe_pods", "evalplane_observe_events",
			"evalplane_get_telemetry", "evalplane_scale_workload", "evalplane_restart_pod", "evalplane_no_op",
		},
	}
	mcpJSON, _ := json.Marshal(mcp)
	return []string{string(viewJSON), string(gatewayJSON), string(mcpJSON)}
}

// Sentinels are evaluator-only markers that must never appear agent-side:
// the reference hash, the variant hash, and the ground-truth hash.
func (t Task) Sentinels() []string {
	version := t.Version()
	ref, _ := json.Marshal(t.ReferenceInput())
	variants, _ := json.Marshal(t.Variants)
	return []string{
		"srequal-sentinel-reference-" + shortHash(version.ID+"reference"+string(ref)),
		"srequal-sentinel-variant-" + shortHash(version.ID+"variant"+string(variants)),
		"srequal-sentinel-groundtruth-" + shortHash(version.ID+"groundtruth"+string(t.GroundTruth)),
	}
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

// GoodInput is the known-good reference outcome: it must pass the grader.
// BadInput is the known-bad unresolved state: it must fail the grader.
func (t Task) GoodInput(runID string) evaluator.EvaluationInput {
	return scenariocertification.BuildEvaluationInput(t.Version(), runID, true, nil)
}

// BadInput is the known-bad unresolved state: it must fail the grader.
func (t Task) BadInput(runID string) evaluator.EvaluationInput {
	return scenariocertification.BuildEvaluationInput(t.Version(), runID, false, nil)
}

// SafetyInput is the representative forbidden outcome for safety detection.
func (t Task) SafetyInput(runID string) evaluator.EvaluationInput {
	return scenariocertification.BuildSafetyInput(t.Version(), runID)
}

// Fixture builds the deterministic fake-certification fixture for this task:
// good must pass, bad must fail, safety must be detected when required,
// telemetry/repeatability/cleanliness/variants/leakage all validated. No live
// cluster is needed. This is focused certification validation, not
// live-runtime certification (see Availability and certification notes).
func (t Task) Fixture() scenariocertification.Fixture {
	version := t.Version()
	good := t.GoodInput("11111111-1111-4111-8111-111111111111")
	bad := t.BadInput("22222222-2222-4222-8222-222222222222")
	counter := t.BadInput("33333333-3333-4333-8333-333333333333")
	safety := t.SafetyInput("44444444-4444-4444-8444-444444444444")
	variants := make([]scenariocertification.VariantCheck, 0, len(t.Variants))
	for i, v := range t.Variants {
		raw, _ := json.Marshal(v.Params)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		variants = append(variants, scenariocertification.VariantCheck{
			VariantID: fmt.Sprintf("srequal-variant-%s-%d", shortHash(t.ID+fmt.Sprint(i)), i),
			Seed:      int64(1000 + i), Config: decoded,
			BoundsValid: true, Solvable: true, EvaluatorValid: true, HiddenPreserved: true,
		})
	}
	fixture := scenariocertification.Fixture{
		ScenarioVersion: version, Reference: t.ReferenceInput(),
		TaskFamilyID: t.Family, CaseType: t.CaseType,
		InitialBroken: true, InitialReason: t.InitialReason,
		GoodInput: good, BadInput: bad, CounterfactualInput: &counter,
		TelemetryRequired:  telemetryRequired(version),
		TelemetryAvailable: true,
		Repetitions:        []scenariocertification.Repetition{{Outcome: "pass", Clean: true}, {Outcome: "pass", Clean: true}},
		EnvironmentClean:   []bool{true, true}, Variants: variants,
		AgentVisiblePayloads: t.AgentPayloads(), Sentinels: t.Sentinels(),
	}
	if len(t.ForbiddenBehaviors) > 0 {
		fixture.SafetyRequired = true
		fixture.SafetyInput = &safety
	}
	return fixture
}

// Certify runs deterministic fake certification for this task.
func (t Task) Certify() scenariocertification.Outcome {
	return scenariocertification.Certify(t.Fixture())
}

func telemetryRequired(version scenario.Version) bool {
	for _, observable := range version.Observables {
		switch observable {
		case "request_rate", "request_rate_rps", "error_rate", "error_rate_pct",
			"p95_latency", "p95_latency_ms", "availability", "availability_pct",
			"telemetry_available":
			return true
		}
	}
	for _, c := range version.Criteria {
		switch canonicalMetric(c.MetricKey) {
		case "request_rate", "error_rate", "p95_latency", "availability":
			return true
		}
	}
	return false
}

func canonicalMetric(key string) string {
	switch key {
	case "request_rate", "request_rate_rps", "application.request_rate_rps":
		return "request_rate"
	case "error_rate", "error_rate_pct", "application.error_rate_pct":
		return "error_rate"
	case "p95_latency", "p95_latency_ms", "application.p95_latency_ms":
		return "p95_latency"
	case "availability", "availability_pct", "application.availability_pct":
		return "availability"
	default:
		return key
	}
}

// Availability computes task availability from environment scheduler truth,
// never from a separate hardcoded flag. Runnable namespace tasks are
// available in the development environment; dedicated-only concepts report
// no eligible environment.
func (t Task) Availability() (runnable bool, profileID, reason string) {
	decision := environment.SelectForRequirements(
		context.Background(), t.Requirements(), "trial", "", environment.DefaultAvailability())
	if decision.Profile == nil || decision.Provider == nil {
		return false, "", decision.ReasonCode
	}
	return true, decision.Profile.ID, decision.ReasonCode
}

// SafeView is the API-safe projection: name, domain/family, case type,
// capabilities exercised, environment requirements, scheduler-derived
// availability, and an honest certification note. Evaluator-only solution
// data (ground truth, reference remediation, hidden variants, conditions
// internals) is never included.
type SafeView struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Domain             string   `json:"domain"`
	TaskFamily         string   `json:"task_family"`
	CaseType           string   `json:"case_type"`
	Difficulty         string   `json:"difficulty"`
	Duration           string   `json:"duration"`
	Capabilities       []string `json:"capabilities"`
	RequiredComponents []string `json:"required_components"`
	MinimumIsolation   string   `json:"minimum_isolation_tier"`
	RuntimeContract    string   `json:"runtime_contract_version"`
	Runnable           bool     `json:"runnable"`
	EligibleProfile    string   `json:"eligible_profile,omitempty"`
	SchedulerReason    string   `json:"scheduler_reason"`
	CertificationNote  string   `json:"certification_note"`
}

// Safe returns the agent-safe projection of this task.
func (t Task) Safe() SafeView {
	req := t.Requirements()
	runnable, profile, reason := t.Availability()
	note := "Focused certification validation passed; live-runtime certification pending real-cluster proof."
	if t.Planned {
		note = "Requires Dedicated Kubernetes — Not yet available. Never executes through the namespace profile."
	}
	return SafeView{
		ID: t.ID, Name: t.Name, Description: t.Description, Domain: t.Domain,
		TaskFamily: t.Family, CaseType: t.CaseType, Difficulty: t.Difficulty, Duration: t.Duration,
		Capabilities:       append([]string(nil), t.Capabilities...),
		RequiredComponents: append([]string(nil), req.RequiredComponents...),
		MinimumIsolation:   req.MinimumIsolation, RuntimeContract: req.RuntimeContract,
		Runnable: runnable, EligibleProfile: profile, SchedulerReason: reason,
		CertificationNote: note,
	}
}

// platformInvalidInput builds an evaluation input carrying a platform failure
// marker (Evalplane-side telemetry outage). The grader must report
// inconclusive (platform-invalid), never agent failure.
func platformInvalidInput(version scenario.Version, runID string) evaluator.EvaluationInput {
	input := scenariocertification.BuildEvaluationInput(version, runID, true, nil)
	// Stay inside the run window so normalization keeps the marker event:
	// BuildEvaluationInput windows on StartedAt..CompletedAt.
	at := input.Run.CreatedAt
	if input.Run.StartedAt != nil {
		at = *input.Run.StartedAt
	}
	// classification:"platform" alone marks RuntimeFailure in normalization.
	input.Events = append(input.Events, evidence.RunEvent{ID: 99, RunID: runID, Sequence: 99,
		EventType: "system", Summary: "environment_provisioning_failed", OccurredAt: at,
		Payload: json.RawMessage(`{"code":"environment_provisioning_failed","classification":"platform"}`), CreatedAt: at})
	return input
}
