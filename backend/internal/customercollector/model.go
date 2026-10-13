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
	ContractVersion = "customer-kubernetes-observation:v1"
	ProviderID      = "customer-kubernetes-collector:v1"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)

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
