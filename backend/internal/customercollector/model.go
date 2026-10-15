// Package customercollector implements tenant-scoped, read-only customer
// Kubernetes collection. Collector snapshots enter the existing run event
// stream and are evaluated by the canonical evaluator/verifier.
package customercollector

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

const (
	ContractVersion            = "customer-kubernetes-observation:v1"
	ProviderID                 = "customer-kubernetes-collector:v1"
	ActionClaimContractVersion = "customer-agent-action-claim:v1"
	ActionClaimTrust           = "UNVERIFIED_AGENT_CLAIM"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
var resourceName = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$`)
var resourceUID = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)
var traceReference = regexp.MustCompile(`^[A-Fa-f0-9]{16,64}$`)

type Environment struct {
	ID                string     `json:"id"`
	WorkspaceID       string     `json:"workspace_id"`
	Name              string     `json:"name"`
	ClusterIdentifier string     `json:"cluster_identifier"`
	Namespaces        []string   `json:"namespaces"`
	ResourceKinds     []string   `json:"resource_kinds"`
	Tier              string     `json:"environment_tier"`
	ConnectionStatus  string     `json:"connection_status"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Session struct {
	ID                       string    `json:"id"`
	WorkspaceID              string    `json:"workspace_id"`
	EnvironmentID            string    `json:"environment_id"`
	RunID                    string    `json:"run_id"`
	Status                   string    `json:"status"`
	ObservationWindowSeconds int       `json:"observation_window_seconds"`
	CreatedAt                time.Time `json:"created_at"`
}

type CreateEnvironmentInput struct {
	Name              string   `json:"name"`
	ClusterIdentifier string   `json:"cluster_identifier"`
	Namespaces        []string `json:"namespaces"`
	Tier              string   `json:"environment_tier"`
}

type CreateSessionInput struct {
	AgentVersionID    string `json:"agent_version_id"`
	ScenarioVersionID string `json:"scenario_version_id"`
}

type EvidenceBatch struct {
	BatchID       string                   `json:"batch_id"`
	Sequence      int64                    `json:"sequence"`
	SchemaVersion string                   `json:"schema_version"`
	Phase         string                   `json:"phase"`
	CapturedAt    time.Time                `json:"captured_at"`
	Observation   agentgateway.Observation `json:"observation"`
}

// ActionClaim is an authenticated customer's report of an external agent action.
// It is deliberately separate from evidence.AgentAction and never establishes
// executor success or causal attribution by itself.
type ActionClaim struct {
	ContractVersion string          `json:"contract_version"`
	WorkspaceID     string          `json:"workspace_id"`
	EnvironmentID   string          `json:"environment_id"`
	SessionID       string          `json:"session_id"`
	RunID           string          `json:"run_id"`
	AgentVersionID  string          `json:"agent_version_id"`
	ClaimID         string          `json:"claim_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	ActionType      string          `json:"action_type"`
	Resource        ClaimedResource `json:"resource"`
	StartedAt       time.Time       `json:"started_at"`
	CompletedAt     time.Time       `json:"completed_at"`
	Outcome         string          `json:"outcome"`
	ExternalTrace   string          `json:"external_trace_reference,omitempty"`
	SubmittedAt     time.Time       `json:"submitted_at"`
	SubmitterUserID string          `json:"submitter_user_id"`
	Trust           string          `json:"trust_classification"`
	Duplicate       bool            `json:"duplicate,omitempty"`
}

type ClaimedResource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

// SubmitActionClaimInput contains only caller assertions. Workspace,
// environment, session, run, submitter and trust are derived by the service.
type SubmitActionClaimInput struct {
	ContractVersion string          `json:"contract_version"`
	AgentVersionID  string          `json:"agent_version_id"`
	ClaimID         string          `json:"claim_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	ActionType      string          `json:"action_type"`
	Resource        ClaimedResource `json:"resource"`
	StartedAt       time.Time       `json:"started_at"`
	CompletedAt     time.Time       `json:"completed_at"`
	Outcome         string          `json:"outcome"`
	ExternalTrace   string          `json:"external_trace_reference,omitempty"`
}

func validateActionClaim(in SubmitActionClaimInput) (SubmitActionClaimInput, error) {
	invalid := func(message string) (SubmitActionClaimInput, error) {
		return in, fmt.Errorf("%w: %s", identity.ErrValidation, message)
	}
	if in.ContractVersion != ActionClaimContractVersion {
		return invalid("unsupported action claim contract version")
	}
	if identity.ValidateUUID(in.AgentVersionID) != nil || identity.ValidateUUID(in.ClaimID) != nil || identity.ValidateUUID(in.IdempotencyKey) != nil {
		return invalid("agent_version_id, claim_id, and idempotency_key must be UUIDs")
	}
	switch in.ActionType {
	case "set_workload_image", "set_live_workload_image", "set_desired_workload_image", "scale_workload", "restart_pod":
	default:
		return invalid("action_type is unsupported")
	}
	if (in.ActionType == "restart_pod" && in.Resource.Kind != "Pod") || (in.ActionType != "restart_pod" && in.Resource.Kind != "Deployment") {
		return invalid("resource kind does not match action_type")
	}
	if !dnsLabel.MatchString(in.Resource.Namespace) || len(in.Resource.Name) > 253 || !resourceName.MatchString(in.Resource.Name) || !resourceUID.MatchString(in.Resource.UID) {
		return invalid("resource must contain a valid Kubernetes kind, namespace, name, and UID")
	}
	if in.StartedAt.IsZero() || in.CompletedAt.IsZero() || in.CompletedAt.Before(in.StartedAt) || in.CompletedAt.Sub(in.StartedAt) > 24*time.Hour {
		return invalid("claimed action timestamps are invalid")
	}
	switch in.Outcome {
	case "proposed", "authorized", "denied", "executed", "failed":
	default:
		return invalid("outcome must be proposed, authorized, denied, executed, or failed")
	}
	in.ExternalTrace = strings.TrimSpace(in.ExternalTrace)
	if in.ExternalTrace != "" && !traceReference.MatchString(in.ExternalTrace) {
		return invalid("external_trace_reference must be a bounded opaque reference")
	}
	in.StartedAt = in.StartedAt.UTC()
	in.CompletedAt = in.CompletedAt.UTC()
	return in, nil
}

func validateActionClaimClock(in SubmitActionClaimInput, now time.Time) error {
	if in.StartedAt.Before(now.Add(-24*time.Hour)) || in.CompletedAt.After(now.Add(30*time.Second)) {
		return fmt.Errorf("%w: claimed action timestamps are outside the accepted clock-skew window", identity.ErrValidation)
	}
	return nil
}

func normalizeEnvironment(in CreateEnvironmentInput) (CreateEnvironmentInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.ClusterIdentifier = strings.TrimSpace(in.ClusterIdentifier)
	in.Tier = strings.TrimSpace(in.Tier)
	if len(in.Name) < 1 || len(in.Name) > 120 || len(in.ClusterIdentifier) < 1 || len(in.ClusterIdentifier) > 200 {
		return in, fmt.Errorf("%w: name and cluster_identifier are required and length-limited", identity.ErrValidation)
	}
	if in.Tier != "staging" && in.Tier != "production" {
		return in, fmt.Errorf("%w: environment_tier must be staging or production", identity.ErrValidation)
	}
	if len(in.Namespaces) == 0 || len(in.Namespaces) > 100 {
		return in, fmt.Errorf("%w: namespaces must contain 1 to 100 entries", identity.ErrValidation)
	}
	seen := map[string]bool{}
	for i, namespace := range in.Namespaces {
		namespace = strings.TrimSpace(namespace)
		if !dnsLabel.MatchString(namespace) || seen[namespace] {
			return in, fmt.Errorf("%w: namespace must be a unique Kubernetes namespace name", identity.ErrValidation)
		}
		seen[namespace] = true
		in.Namespaces[i] = namespace
	}
	return in, nil
}

func validateBatch(in EvidenceBatch, now time.Time) error {
	if identity.ValidateUUID(in.BatchID) != nil || in.Sequence < 1 {
		return fmt.Errorf("%w: batch_id and positive sequence are required", identity.ErrValidation)
	}
	if in.SchemaVersion != ContractVersion {
		return fmt.Errorf("%w: unsupported evidence schema version", identity.ErrValidation)
	}
	if in.Phase != "baseline" && in.Phase != "stability" && in.Phase != "final" {
		return fmt.Errorf("%w: phase must be baseline, stability, or final", identity.ErrValidation)
	}
	if in.CapturedAt.IsZero() || in.CapturedAt.Before(now.Add(-5*time.Minute)) || in.CapturedAt.After(now.Add(30*time.Second)) {
		return fmt.Errorf("%w: captured_at is outside the accepted clock-skew window", identity.ErrValidation)
	}
	if in.Observation.Timestamp.IsZero() || !in.Observation.Timestamp.Equal(in.CapturedAt) {
		return fmt.Errorf("%w: observation timestamp must match captured_at", identity.ErrValidation)
	}
	if len(in.Observation.Workloads)+len(in.Observation.Pods)+len(in.Observation.Autoscalers) > 2000 {
		return fmt.Errorf("%w: observation exceeds resource limit", identity.ErrValidation)
	}
	if len(in.Observation.RecentEvents) > 0 {
		return fmt.Errorf("%w: event evidence is not supported by this schema", identity.ErrValidation)
	}
	if in.Observation.Metrics != nil && len(in.Observation.Metrics) > 0 {
		return fmt.Errorf("%w: telemetry is not supported by this collector schema", identity.ErrValidation)
	}
	seen := map[string]bool{}
	for _, workload := range in.Observation.Workloads {
		if workload.Kind != "Deployment" || !validResourceName(workload.Name, workload.Namespace) || workload.DesiredReplicas < 0 || workload.ReadyReplicas < 0 || workload.AvailableReplicas < 0 || workload.UpdatedReplicas < 0 {
			return fmt.Errorf("%w: invalid workload observation", identity.ErrValidation)
		}
		key := workload.Kind + "/" + workload.Namespace + "/" + workload.Name
		if seen[key] {
			return fmt.Errorf("%w: duplicate workload identity", identity.ErrValidation)
		}
		seen[key] = true
	}
	for _, pod := range in.Observation.Pods {
		if !validResourceName(pod.Name, pod.Namespace) || pod.RestartCount < 0 {
			return fmt.Errorf("%w: invalid pod observation", identity.ErrValidation)
		}
		key := "Pod/" + pod.Namespace + "/" + pod.Name
		if seen[key] {
			return fmt.Errorf("%w: duplicate pod identity", identity.ErrValidation)
		}
		seen[key] = true
	}
	for _, hpa := range in.Observation.Autoscalers {
		if !validResourceName(hpa.Name, hpa.Namespace) || hpa.TargetKind != "Deployment" || !dnsLabel.MatchString(hpa.TargetName) || hpa.MinReplicas < 0 || hpa.MaxReplicas < hpa.MinReplicas {
			return fmt.Errorf("%w: invalid autoscaler observation", identity.ErrValidation)
		}
	}
	return nil
}

func validResourceName(name, namespace string) bool {
	return dnsLabel.MatchString(name) && dnsLabel.MatchString(namespace)
}

func validateOrder(expected, batchCount int64, lastPhase string, lastCaptured *time.Time, in EvidenceBatch) error {
	if in.Sequence != expected {
		return fmt.Errorf("%w: expected sequence %d", identity.ErrConflict, expected)
	}
	if lastCaptured != nil && !in.CapturedAt.After(*lastCaptured) {
		return fmt.Errorf("%w: captured_at must increase strictly", identity.ErrConflict)
	}
	if (batchCount == 0 && in.Phase != "baseline") || (batchCount > 0 && in.Phase == "baseline") || (in.Phase == "final" && batchCount < 3) || lastPhase == "final" {
		return fmt.Errorf("%w: invalid observation phase order", identity.ErrConflict)
	}
	return nil
}

func validateObservationWindow(received, baselineReceived time.Time, seconds int) error {
	if seconds < 1 {
		return validation("observation window must be positive")
	}
	if received.Before(baselineReceived.Add(time.Duration(seconds) * time.Second)) {
		return fmt.Errorf("%w: configured observation window has not elapsed", identity.ErrConflict)
	}
	return nil
}

func newCredential() (string, []byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, errors.New("generate collector credential")
	}
	token := "rvxc_" + base64.RawURLEncoding.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func tokenDigest(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func validToken(token string) bool { return strings.HasPrefix(token, "rvxc_") && len(token) == 48 }

func resourceScope(kinds []string, namespaces []string, in EvidenceBatch) error {
	ns := make(map[string]bool, len(namespaces))
	for _, value := range namespaces {
		ns[value] = true
	}
	kind := make(map[string]bool, len(kinds))
	for _, value := range kinds {
		kind[value] = true
	}
	for _, resource := range in.Observation.Workloads {
		if !ns[resource.Namespace] || !kind[resource.Kind] {
			return fmt.Errorf("%w: workload is outside collector resource scope", auth.ErrForbidden)
		}
	}
	for _, resource := range in.Observation.Pods {
		if !ns[resource.Namespace] || !kind["Pod"] {
			return fmt.Errorf("%w: pod is outside collector resource scope", auth.ErrForbidden)
		}
	}
	for _, resource := range in.Observation.Autoscalers {
		if !ns[resource.Namespace] || !kind["HorizontalPodAutoscaler"] {
			return fmt.Errorf("%w: autoscaler is outside collector resource scope", auth.ErrForbidden)
		}
	}
	return nil
}
