package operational

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DirectSourceConfig names the customer-approved read-only API and query
// semantics. No bearer token or private key is persisted in this record.
type DirectSourceConfig struct {
	Repository    string            `json:"repository,omitempty"`
	Path          string            `json:"path,omitempty"`
	Revision      string            `json:"revision,omitempty"`
	ProbeVantage  string            `json:"probe_vantage,omitempty"`
	Queries       map[string]string `json:"queries,omitempty"`
	MinRequests   int64             `json:"min_requests,omitempty"`
	WindowSeconds int               `json:"window_seconds,omitempty"`
}
type DirectSource struct {
	ID                      string             `json:"id"`
	WorkspaceID             string             `json:"workspace_id"`
	EnvironmentID           string             `json:"environment_id"`
	Kind                    string             `json:"source_kind"`
	EndpointURL             string             `json:"endpoint_url"`
	ServerCertificateSHA256 string             `json:"server_certificate_sha256"`
	Namespace               string             `json:"namespace"`
	ResourceName            string             `json:"resource_name"`
	Configuration           DirectSourceConfig `json:"configuration"`
	CreatedAt               time.Time          `json:"created_at"`
	ExpiresAt               time.Time          `json:"expires_at"`
	RevokedAt               *time.Time         `json:"revoked_at,omitempty"`
	EvidenceCount           int64              `json:"evidence_count"`
	LastReceivedAt          *time.Time         `json:"last_received_at,omitempty"`
}
type DirectObservation struct {
	ID                string          `json:"id"`
	SourceID          string          `json:"source_id"`
	Kind              string          `json:"source_kind"`
	SessionID         string          `json:"session_id"`
	SourceDigest      string          `json:"source_digest"`
	ObservedAt        time.Time       `json:"observed_at"`
	AcquiredAt        time.Time       `json:"acquired_at"`
	Payload           json.RawMessage `json:"payload"`
	MonitoringCycleID string          `json:"monitoring_cycle_id,omitempty"`
}

// KubernetesResourceState is a read-only API observation of one registered
// object. Exposure fields describe configuration, never external reachability.
type KubernetesResourceState struct {
	Kind                string   `json:"kind"`
	Namespace           string   `json:"namespace"`
	Name                string   `json:"name"`
	UID                 string   `json:"uid,omitempty"`
	ResourceVersion     string   `json:"resource_version,omitempty"`
	Present             bool     `json:"present"`
	ServiceType         string   `json:"service_type,omitempty"`
	ExternalIPs         []string `json:"external_ips,omitempty"`
	LoadBalancerIngress []string `json:"load_balancer_ingress,omitempty"`
	IngressHosts        []string `json:"ingress_hosts,omitempty"`
	IngressBackends     []string `json:"ingress_backends,omitempty"`
	NetworkPolicyTypes  []string `json:"network_policy_types,omitempty"`
	AllowsAllIngress    bool     `json:"allows_all_ingress,omitempty"`
}

func validDirectURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == ""
}
func validDirectConfig(x DirectSource) bool {
	if x.Namespace == "" || x.ResourceName == "" || (x.Kind != "external_reachability" && !validDirectURL(x.EndpointURL)) {
		return false
	}
	digest, err := hex.DecodeString(x.ServerCertificateSHA256)
	if err != nil || len(digest) != sha256.Size {
		return false
	}
	switch x.Kind {
	case "kubernetes_state", "kubernetes_service", "kubernetes_ingress", "kubernetes_networkpolicy":
		return true
	case "external_reachability":
		return validReachabilityTarget(x.EndpointURL) && validProbeVantage(x.Configuration.ProbeVantage)
	case "argocd":
		return x.Configuration.Repository != "" && x.Configuration.Path != "" && x.Configuration.Revision != ""
	case "prometheus":
		if x.Configuration.MinRequests <= 0 || x.Configuration.WindowSeconds < 30 || x.Configuration.WindowSeconds > 3600 {
			return false
		}
		if x.Configuration.Queries["request_count"] == "" {
			return false
		}
		count := 0
		for _, key := range []string{"error_rate_pct", "request_success_rate_pct", "p95_latency_ms"} {
			if x.Configuration.Queries[key] != "" {
				count++
			}
		}
		return count > 0
	}
	return false
}
func (s *Service) RegisterDirectSource(ctx context.Context, p auth.Principal, w, e string, in DirectSource) (DirectSource, error) {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return DirectSource{}, err
	}
	if role != "owner" && role != "admin" {
		return DirectSource{}, auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || !validDirectConfig(in) {
		return DirectSource{}, identity.ErrValidation
	}
	digest, _ := hex.DecodeString(strings.ToLower(in.ServerCertificateSHA256))
	config, _ := json.Marshal(in.Configuration)
	var namespaces, kinds []string
	err = s.pool.QueryRow(ctx, `SELECT namespaces,resource_kinds FROM customer_kubernetes_environments WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL`, w, e).Scan(&namespaces, &kinds)
	if err != nil {
		return DirectSource{}, auth.ErrForbidden
	}
	resourceKind := map[string]string{"kubernetes_state": "Deployment", "kubernetes_service": "Service", "kubernetes_ingress": "Ingress", "kubernetes_networkpolicy": "NetworkPolicy", "external_reachability": "Ingress"}[in.Kind]
	if !containsResource(namespaces, in.Namespace) || resourceKind != "" && !containsResource(kinds, resourceKind) {
		return DirectSource{}, auth.ErrForbidden
	}
	in.WorkspaceID = w
	in.EnvironmentID = e
	in.ServerCertificateSHA256 = hex.EncodeToString(digest)
	err = s.pool.QueryRow(ctx, `INSERT INTO customer_outcome_sources(workspace_id,environment_id,source_kind,endpoint_url,server_certificate_sha256,namespace,resource_name,configuration,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9) RETURNING id,created_at,expires_at`, w, e, in.Kind, in.EndpointURL, digest, in.Namespace, in.ResourceName, string(config), p.UserID).Scan(&in.ID, &in.CreatedAt, &in.ExpiresAt)
	return in, err
}
func containsResource(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
func (s *Service) ListDirectSources(ctx context.Context, p auth.Principal, w, e string) ([]DirectSource, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.workspace_id,o.environment_id,o.source_kind,o.endpoint_url,o.server_certificate_sha256,o.namespace,o.resource_name,o.configuration,o.created_at,o.expires_at,o.revoked_at,(SELECT count(*) FROM customer_direct_outcome_observations d WHERE d.source_id=o.id),(SELECT max(d.acquired_at) FROM customer_direct_outcome_observations d WHERE d.source_id=o.id) FROM customer_outcome_sources o WHERE o.workspace_id=$1 AND o.environment_id=$2 ORDER BY o.created_at DESC,o.id`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DirectSource{}
	for rows.Next() {
		var x DirectSource
		var fingerprint, data []byte
		if err = rows.Scan(&x.ID, &x.WorkspaceID, &x.EnvironmentID, &x.Kind, &x.EndpointURL, &fingerprint, &x.Namespace, &x.ResourceName, &data, &x.CreatedAt, &x.ExpiresAt, &x.RevokedAt, &x.EvidenceCount, &x.LastReceivedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &x.Configuration); err != nil {
			return nil, err
		}
		x.ServerCertificateSHA256 = hex.EncodeToString(fingerprint)
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) RevokeDirectSource(ctx context.Context, p auth.Principal, w, e, id string) error {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(id) != nil {
		return identity.ErrValidation
	}
	tag, err := s.pool.Exec(ctx, `UPDATE customer_outcome_sources SET revoked_at=now() WHERE id=$1 AND workspace_id=$2 AND environment_id=$3 AND revoked_at IS NULL`, id, w, e)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return auth.ErrForbidden
	}
	return nil
}

// AcquireDirect is called by the separately deployed Realverax-owned read-only
// acquirer. It never accepts caller-provided observations: it fetches them
// directly over a normal CA-verified TLS connection and pins the leaf cert.
func (s *Service) AcquireDirect(ctx context.Context, sourceID, sessionID string, caPEM []byte, bearer string) (DirectObservation, error) {
	return s.acquireDirect(ctx, sourceID, sessionID, "", caPEM, bearer, "")
}

// AcquireReachability runs only from a separately operated external vantage.
// The configured vantage identity must match the customer registration.
func (s *Service) AcquireReachability(ctx context.Context, sourceID, sessionID, vantage string) (DirectObservation, error) {
	return s.acquireDirect(ctx, sourceID, sessionID, "", nil, "", vantage)
}

// AcquireDirectCycle reuses a persisted observation when a worker retries the
// same logical cycle after a process restart. It cannot be used outside a
// claimed monitoring plan.
func (s *Service) AcquireDirectCycle(ctx context.Context, sourceID, sessionID, cycleID string, caPEM []byte, bearer string) (DirectObservation, error) {
	if identity.ValidateUUID(cycleID) != nil {
		return DirectObservation{}, identity.ErrValidation
	}
	return s.acquireDirect(ctx, sourceID, sessionID, cycleID, caPEM, bearer, "")
}

func (s *Service) AcquireReachabilityCycle(ctx context.Context, sourceID, sessionID, cycleID, vantage string) (DirectObservation, error) {
	if identity.ValidateUUID(cycleID) != nil {
		return DirectObservation{}, identity.ErrValidation
	}
	return s.acquireDirect(ctx, sourceID, sessionID, cycleID, nil, "", vantage)
}

func (s *Service) acquireDirect(ctx context.Context, sourceID, sessionID, cycleID string, caPEM []byte, bearer, vantage string) (DirectObservation, error) {
	var out DirectObservation
	if identity.ValidateUUID(sourceID) != nil || identity.ValidateUUID(sessionID) != nil {
		return out, identity.ErrValidation
	}
	var x DirectSource
	var cert, data []byte
	var run, status string
	var revoked, envRevoked *time.Time
	err := s.pool.QueryRow(ctx, `SELECT o.id,o.workspace_id,o.environment_id,o.source_kind,o.endpoint_url,o.server_certificate_sha256,o.namespace,o.resource_name,o.configuration,o.expires_at,o.revoked_at,s.run_id,s.status,e.revoked_at FROM customer_outcome_sources o JOIN customer_observation_sessions s ON s.workspace_id=o.workspace_id AND s.environment_id=o.environment_id JOIN customer_kubernetes_environments e ON e.workspace_id=o.workspace_id AND e.id=o.environment_id WHERE o.id=$1 AND s.id=$2`, sourceID, sessionID).Scan(&x.ID, &x.WorkspaceID, &x.EnvironmentID, &x.Kind, &x.EndpointURL, &cert, &x.Namespace, &x.ResourceName, &data, &x.ExpiresAt, &revoked, &run, &status, &envRevoked)
	if err != nil || revoked != nil || envRevoked != nil || !s.now().Before(x.ExpiresAt) {
		return out, auth.ErrForbidden
	}
	if cycleID == "" && status != "active" {
		return out, auth.ErrForbidden
	}
	if err = json.Unmarshal(data, &x.Configuration); err != nil {
		return out, err
	}
	if x.Kind == "external_reachability" {
		if !validProbeVantage(vantage) || vantage != x.Configuration.ProbeVantage || bearer != "" || len(caPEM) != 0 {
			return out, auth.ErrForbidden
		}
	} else if vantage != "" {
		return out, identity.ErrValidation
	}
	if cycleID != "" {
		var authorized bool
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assurance_monitoring_plans p JOIN assurance_monitoring_plan_sources ps ON ps.session_id=p.session_id AND ps.source_id=$2 WHERE p.session_id=$1 AND p.workspace_id=$3 AND p.environment_id=$4 AND p.current_cycle_id=$5 AND p.state='running' AND p.lease_until>now())`, sessionID, sourceID, x.WorkspaceID, x.EnvironmentID, cycleID).Scan(&authorized)
		if err != nil || !authorized || status != "completed" {
			return out, auth.ErrForbidden
		}
		var priorDigest []byte
		err = s.pool.QueryRow(ctx, `SELECT id,source_digest,observed_at,acquired_at,payload FROM customer_direct_outcome_observations WHERE source_id=$1 AND session_id=$2 AND monitoring_cycle_id=$3`, sourceID, sessionID, cycleID).Scan(&out.ID, &priorDigest, &out.ObservedAt, &out.AcquiredAt, &out.Payload)
		if err == nil {
			out.SourceID = sourceID
			out.SessionID = sessionID
			out.Kind = x.Kind
			out.MonitoringCycleID = cycleID
			out.SourceDigest = "sha256:" + hex.EncodeToString(priorDigest)
			return out, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return DirectObservation{}, err
		}
	}
	x.ServerCertificateSHA256 = hex.EncodeToString(cert)
	if x.Kind == "external_reachability" {
		if err = s.reachabilityTargetConfigured(ctx, x, sessionID, s.now()); err != nil {
			return out, err
		}
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return out, err
	}
	if len(caPEM) > 0 && !roots.AppendCertsFromPEM(caPEM) {
		return out, identity.ErrValidation
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("source server certificate missing")
		}
		fingerprint := sha256.Sum256(state.PeerCertificates[0].Raw)
		if subtle.ConstantTimeCompare(fingerprint[:], cert) != 1 {
			return errors.New("source certificate pin mismatch")
		}
		return nil
	}}}
	resolvedProbeIP := ""
	if x.Kind == "external_reachability" {
		target, _ := url.Parse(x.EndpointURL)
		dialer := &net.Dialer{Timeout: 3 * time.Second}
		transport.Proxy = nil
		transport.DisableKeepAlives = true
		transport.DialContext = probeDialContext(net.DefaultResolver.LookupIP, dialer.DialContext, target.Hostname(), func(ip string) { resolvedProbeIP = ip })
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("source redirect forbidden") }}
	if x.Kind == "external_reachability" {
		client.Timeout = 5 * time.Second
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return errProbeRedirect }
	}
	id := uuid.NewString()
	now := s.now().UTC()
	var raw []byte
	var payload any
	var observed time.Time
	switch x.Kind {
	case "kubernetes_state", "kubernetes_service", "kubernetes_ingress", "kubernetes_networkpolicy":
		raw, payload, observed, err = acquireKubernetes(ctx, client, x, bearer, now)
	case "argocd":
		raw, payload, observed, err = s.acquireArgo(ctx, client, x, bearer, now, id)
	case "prometheus":
		raw, payload, observed, err = acquirePrometheus(ctx, client, x, bearer, now)
	case "external_reachability":
		raw, payload, observed, err = acquireReachability(ctx, client, x, vantage, &resolvedProbeIP, now)
		now = s.now().UTC()
	default:
		return out, identity.ErrValidation
	}
	if err != nil {
		return out, err
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(raw)
	out = DirectObservation{ID: id, SourceID: sourceID, Kind: x.Kind, SessionID: sessionID, SourceDigest: "sha256:" + hex.EncodeToString(digest[:]), ObservedAt: observed, AcquiredAt: now, Payload: normalized, MonitoringCycleID: cycleID}
	eventID := id
	if cycleID != "" {
		eventID = cycleID
	}
	var storedDigest []byte
	err = s.pool.QueryRow(ctx, `INSERT INTO customer_direct_outcome_observations(id,workspace_id,environment_id,session_id,run_id,source_id,source_kind,source_event_id,source_digest,source_raw,payload,observed_at,acquired_at,server_certificate_sha256,monitoring_cycle_id) SELECT $1,$2,$3,s.id,s.run_id,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,NULLIF($14,'')::uuid FROM customer_observation_sessions s JOIN customer_outcome_sources o ON o.id=$5 AND o.workspace_id=s.workspace_id AND o.environment_id=s.environment_id WHERE s.id=$4 AND s.workspace_id=$2 AND s.environment_id=$3 AND (s.status='active' AND $14='' OR s.status='completed' AND $14<>'' AND EXISTS(SELECT 1 FROM assurance_monitoring_plans p WHERE p.session_id=s.id AND p.current_cycle_id=NULLIF($14,'')::uuid AND p.state='running' AND p.lease_until>now())) AND o.revoked_at IS NULL AND o.expires_at>now() ON CONFLICT(source_id,source_event_id) DO NOTHING RETURNING source_digest`, id, x.WorkspaceID, x.EnvironmentID, sessionID, sourceID, x.Kind, eventID, digest[:], raw, string(normalized), observed, now, cert, cycleID).Scan(&storedDigest)
	if cycleID != "" && errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `SELECT id,source_digest,observed_at,acquired_at,payload FROM customer_direct_outcome_observations WHERE source_id=$1 AND session_id=$2 AND monitoring_cycle_id=$3`, sourceID, sessionID, cycleID).Scan(&out.ID, &storedDigest, &out.ObservedAt, &out.AcquiredAt, &out.Payload)
		if err == nil {
			out.SourceDigest = "sha256:" + hex.EncodeToString(storedDigest)
			return out, nil
		}
	}
	if err != nil {
		return DirectObservation{}, err
	}
	return out, nil
}
func getDirect(ctx context.Context, client *http.Client, rawURL, bearer string) ([]byte, error) {
	body, status, err := getDirectStatus(ctx, client, rawURL, bearer)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("direct source HTTP status %d", status)
	}
	return body, nil
}
func getDirectStatus(ctx context.Context, client *http.Client, rawURL, bearer string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > 1<<20 {
		return nil, 0, identity.ErrValidation
	}
	return body, response.StatusCode, nil
}
func acquireKubernetes(ctx context.Context, client *http.Client, x DirectSource, bearer string, now time.Time) ([]byte, any, time.Time, error) {
	if x.Kind != "kubernetes_state" {
		return acquireKubernetesObject(ctx, client, x, bearer, now)
	}
	uri := x.EndpointURL + "/apis/apps/v1/namespaces/" + url.PathEscape(x.Namespace) + "/deployments/" + url.PathEscape(x.ResourceName)
	raw, status, err := getDirectStatus(ctx, client, uri, bearer)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if status == http.StatusNotFound {
		if !kubernetesNotFound(raw) {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		return raw, map[string]any{"resource_state": KubernetesResourceState{Kind: "Deployment", Namespace: x.Namespace, Name: x.ResourceName, Present: false}}, now, nil
	}
	if status != http.StatusOK {
		return nil, nil, time.Time{}, fmt.Errorf("direct source HTTP status %d", status)
	}
	var d struct {
		Metadata struct {
			Name, Namespace, UID, ResourceVersion string
			Generation                            int64
		}
		Spec struct {
			Replicas *int32
			Template struct {
				Spec struct {
					Containers []struct{ Name, Image string }
				}
			}
		}
		Status struct {
			ReadyReplicas, AvailableReplicas, UpdatedReplicas int32
			ObservedGeneration                                int64
		}
	}
	if err = json.Unmarshal(raw, &d); err != nil {
		return nil, nil, time.Time{}, err
	}
	if d.Metadata.Name != x.ResourceName || d.Metadata.Namespace != x.Namespace || d.Metadata.UID == "" || d.Metadata.ResourceVersion == "" || d.Metadata.Generation <= 0 || d.Spec.Replicas == nil {
		return nil, nil, time.Time{}, identity.ErrValidation
	}
	image := ""
	if len(d.Spec.Template.Spec.Containers) == 1 {
		image = d.Spec.Template.Spec.Containers[0].Image
	}
	w := agentgateway.WorkloadObservation{Kind: "Deployment", Name: d.Metadata.Name, Namespace: d.Metadata.Namespace, UID: d.Metadata.UID, ResourceVersion: d.Metadata.ResourceVersion, Generation: d.Metadata.Generation, ObservedGeneration: d.Status.ObservedGeneration, DesiredReplicas: *d.Spec.Replicas, ReadyReplicas: d.Status.ReadyReplicas, AvailableReplicas: d.Status.AvailableReplicas, UpdatedReplicas: d.Status.UpdatedReplicas, Image: image}
	observation := agentgateway.Observation{Timestamp: now, Workloads: []agentgateway.WorkloadObservation{w}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}
	return raw, map[string]any{"observation": observation, "resource_state": KubernetesResourceState{Kind: "Deployment", Namespace: x.Namespace, Name: x.ResourceName, UID: d.Metadata.UID, ResourceVersion: d.Metadata.ResourceVersion, Present: true}}, now, nil
}

func kubernetesNotFound(raw []byte) bool {
	var status struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
		Code   int    `json:"code"`
	}
	return json.Unmarshal(raw, &status) == nil && status.Kind == "Status" && status.Reason == "NotFound" && status.Code == http.StatusNotFound
}

func acquireKubernetesObject(ctx context.Context, client *http.Client, x DirectSource, bearer string, now time.Time) ([]byte, any, time.Time, error) {
	paths := map[string]string{"kubernetes_service": "/api/v1/namespaces/" + url.PathEscape(x.Namespace) + "/services/" + url.PathEscape(x.ResourceName), "kubernetes_ingress": "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(x.Namespace) + "/ingresses/" + url.PathEscape(x.ResourceName), "kubernetes_networkpolicy": "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(x.Namespace) + "/networkpolicies/" + url.PathEscape(x.ResourceName)}
	kinds := map[string]string{"kubernetes_service": "Service", "kubernetes_ingress": "Ingress", "kubernetes_networkpolicy": "NetworkPolicy"}
	path, ok := paths[x.Kind]
	if !ok {
		return nil, nil, time.Time{}, identity.ErrValidation
	}
	raw, status, err := getDirectStatus(ctx, client, x.EndpointURL+path, bearer)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	state := KubernetesResourceState{Kind: kinds[x.Kind], Namespace: x.Namespace, Name: x.ResourceName, Present: status == http.StatusOK}
	if status == http.StatusNotFound {
		if !kubernetesNotFound(raw) {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		return raw, map[string]any{"resource_state": state}, now, nil
	}
	if status != http.StatusOK {
		return nil, nil, time.Time{}, fmt.Errorf("direct source HTTP status %d", status)
	}
	var object struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name            string `json:"name"`
			Namespace       string `json:"namespace"`
			UID             string `json:"uid"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Spec   json.RawMessage `json:"spec"`
		Status json.RawMessage `json:"status"`
	}
	if err = json.Unmarshal(raw, &object); err != nil {
		return nil, nil, time.Time{}, err
	}
	if object.Kind != state.Kind || object.Metadata.Name != x.ResourceName || object.Metadata.Namespace != x.Namespace || object.Metadata.UID == "" || object.Metadata.ResourceVersion == "" || len(object.Spec) == 0 {
		return nil, nil, time.Time{}, identity.ErrValidation
	}
	state.UID = object.Metadata.UID
	state.ResourceVersion = object.Metadata.ResourceVersion
	switch x.Kind {
	case "kubernetes_service":
		var spec struct {
			Type        string   `json:"type"`
			ExternalIPs []string `json:"externalIPs"`
		}
		var result struct {
			LoadBalancer struct {
				Ingress []struct {
					IP       string `json:"ip"`
					Hostname string `json:"hostname"`
				} `json:"ingress"`
			} `json:"loadBalancer"`
		}
		if len(object.Status) == 0 || string(object.Status) == "null" {
			object.Status = json.RawMessage(`{}`)
		}
		if json.Unmarshal(object.Spec, &spec) != nil || spec.Type == "" || json.Unmarshal(object.Status, &result) != nil {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		state.ServiceType = spec.Type
		state.ExternalIPs = spec.ExternalIPs
		for _, entry := range result.LoadBalancer.Ingress {
			if entry.IP != "" {
				state.LoadBalancerIngress = append(state.LoadBalancerIngress, entry.IP)
			}
			if entry.Hostname != "" {
				state.LoadBalancerIngress = append(state.LoadBalancerIngress, entry.Hostname)
			}
		}
	case "kubernetes_ingress":
		var spec struct {
			DefaultBackend struct {
				Service struct {
					Name string `json:"name"`
				} `json:"service"`
			} `json:"defaultBackend"`
			Rules []struct {
				Host string `json:"host"`
				HTTP struct {
					Paths []struct {
						Backend struct {
							Service struct {
								Name string `json:"name"`
							} `json:"service"`
						} `json:"backend"`
					} `json:"paths"`
				} `json:"http"`
			} `json:"rules"`
		}
		if json.Unmarshal(object.Spec, &spec) != nil {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		var ingressStatus struct {
			LoadBalancer struct {
				Ingress []struct {
					IP       string `json:"ip"`
					Hostname string `json:"hostname"`
				} `json:"ingress"`
			} `json:"loadBalancer"`
		}
		if len(object.Status) > 0 && string(object.Status) != "null" && json.Unmarshal(object.Status, &ingressStatus) != nil {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		for _, entry := range ingressStatus.LoadBalancer.Ingress {
			if entry.IP != "" {
				state.LoadBalancerIngress = append(state.LoadBalancerIngress, entry.IP)
			}
			if entry.Hostname != "" {
				state.LoadBalancerIngress = append(state.LoadBalancerIngress, entry.Hostname)
			}
		}
		if spec.DefaultBackend.Service.Name != "" {
			state.IngressBackends = append(state.IngressBackends, spec.DefaultBackend.Service.Name)
		}
		for _, rule := range spec.Rules {
			state.IngressHosts = append(state.IngressHosts, rule.Host)
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service.Name != "" {
					state.IngressBackends = append(state.IngressBackends, path.Backend.Service.Name)
				}
			}
		}
	case "kubernetes_networkpolicy":
		var spec struct {
			PolicyTypes []string `json:"policyTypes"`
			Ingress     []struct {
				From []json.RawMessage `json:"from"`
			} `json:"ingress"`
		}
		if json.Unmarshal(object.Spec, &spec) != nil {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		state.NetworkPolicyTypes = spec.PolicyTypes
		for _, rule := range spec.Ingress {
			if len(rule.From) == 0 {
				state.AllowsAllIngress = true
			}
		}
	}
	return raw, map[string]any{"resource_state": state}, now, nil
}
func (s *Service) acquireArgo(ctx context.Context, client *http.Client, x DirectSource, bearer string, now time.Time, id string) ([]byte, any, time.Time, error) {
	raw, err := getDirect(ctx, client, x.EndpointURL+"/api/v1/applications/"+url.PathEscape(x.ResourceName), bearer)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var app struct {
		Metadata struct{ Name string }
		Spec     struct {
			Source struct{ RepoURL, Path, TargetRevision string }
		}
		Status struct {
			Sync   struct{ Status, Revision string }
			Health struct{ Status string }
		}
	}
	if err = json.Unmarshal(raw, &app); err != nil {
		return nil, nil, time.Time{}, err
	}
	if app.Metadata.Name != x.ResourceName || app.Spec.Source.RepoURL != x.Configuration.Repository || app.Spec.Source.Path != x.Configuration.Path || app.Spec.Source.TargetRevision != x.Configuration.Revision {
		return nil, nil, time.Time{}, identity.ErrValidation
	}
	history, err := s.listContractsInternal(ctx, x.WorkspaceID, x.EnvironmentID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var desired *gitops.DesiredState
	for _, c := range activeContracts(history, now) {
		for _, rule := range c.Rules {
			if rule.Type == "post_image" && rule.Namespace == x.Namespace && rule.ResourceName != "" {
				if desired != nil {
					return nil, nil, time.Time{}, identity.ErrConflict
				}
				desired = &gitops.DesiredState{Repository: x.Configuration.Repository, Revision: x.Configuration.Revision, Path: x.Configuration.Path, Resource: "Deployment/" + rule.Namespace + "/" + rule.ResourceName, Image: rule.Value, CapturedAt: now, Reference: fmt.Sprintf("contract:%s:v%d", c.ID, c.Version)}
			}
		}
	}
	g := gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: &gitops.ArgoState{Application: x.ResourceName, Namespace: x.Namespace, SyncStatus: app.Status.Sync.Status, HealthStatus: app.Status.Health.Status, TargetRevision: app.Spec.Source.TargetRevision, ObservedRevision: app.Status.Sync.Revision, CapturedAt: now, Reference: "direct_outcome:" + id}}
	return raw, map[string]any{"gitops": g}, now, nil
}
func acquirePrometheus(ctx context.Context, client *http.Client, x DirectSource, bearer string, now time.Time) ([]byte, any, time.Time, error) {
	values := map[string]float64{}
	rawParts := map[string]json.RawMessage{}
	var observed time.Time
	for _, key := range []string{"request_count", "error_rate_pct", "request_success_rate_pct", "p95_latency_ms"} {
		query := x.Configuration.Queries[key]
		if query == "" {
			continue
		}
		uri := x.EndpointURL + "/api/v1/query?query=" + url.QueryEscape(query)
		raw, err := getDirect(ctx, client, uri, bearer)
		if err != nil {
			return nil, nil, time.Time{}, err
		}
		var response struct {
			Status string
			Data   struct {
				ResultType string
				Result     []struct{ Value []json.RawMessage }
			}
		}
		if err = json.Unmarshal(raw, &response); err != nil {
			return nil, nil, time.Time{}, err
		}
		if response.Status != "success" || response.Data.ResultType != "vector" || len(response.Data.Result) != 1 || len(response.Data.Result[0].Value) != 2 {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		var ts float64
		var valueText string
		if json.Unmarshal(response.Data.Result[0].Value[0], &ts) != nil || json.Unmarshal(response.Data.Result[0].Value[1], &valueText) != nil {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		value, err := strconv.ParseFloat(valueText, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		if key == "request_count" && math.Trunc(value) != value {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		at := time.UnixMilli(int64(ts * 1000)).UTC()
		if at.After(now.Add(30*time.Second)) || now.Sub(at) > 2*time.Minute {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		if !observed.IsZero() && math.Abs(at.Sub(observed).Seconds()) > 30 {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
		if observed.IsZero() {
			observed = at
		}
		values[key] = value
		rawParts[key] = raw
	}
	if values["request_count"] < float64(x.Configuration.MinRequests) {
		return nil, nil, time.Time{}, identity.ErrValidation
	}
	for _, key := range []string{"error_rate_pct", "request_success_rate_pct"} {
		if v, ok := values[key]; ok && v > 100 {
			return nil, nil, time.Time{}, identity.ErrValidation
		}
	}
	sample := MetricSample{ID: uuid.NewString(), Namespace: x.Namespace, Service: x.ResourceName, SampledAt: observed, AcquiredAt: now, ProvenanceStatus: "SOURCE_AUTHENTICATED", RequestCount: int64(values["request_count"]), WindowStartedAt: observed.Add(-time.Duration(x.Configuration.WindowSeconds) * time.Second)}
	if v, ok := values["error_rate_pct"]; ok {
		sample.ErrorRatePct = &v
	}
	if v, ok := values["request_success_rate_pct"]; ok {
		sample.RequestSuccessRatePct = &v
	}
	if v, ok := values["p95_latency_ms"]; ok {
		sample.P95LatencyMS = &v
	}
	raw, _ := json.Marshal(rawParts)
	return raw, map[string]any{"metric_sample": sample, "query_window_seconds": x.Configuration.WindowSeconds, "minimum_requests": x.Configuration.MinRequests}, observed, nil
}

// ListDirectObservations exposes normalized values and source references only;
// raw upstream API replies and bearer credentials never leave the operator API.
func (s *Service) ListDirectObservations(ctx context.Context, p auth.Principal, w, e, session string) ([]DirectObservation, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(session) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id,source_id,source_kind,session_id,source_digest,observed_at,acquired_at,payload,COALESCE(monitoring_cycle_id::text,'') FROM customer_direct_outcome_observations WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY acquired_at,id`, w, e, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DirectObservation{}
	for rows.Next() {
		var v DirectObservation
		var digest []byte
		if err = rows.Scan(&v.ID, &v.SourceID, &v.Kind, &v.SessionID, &digest, &v.ObservedAt, &v.AcquiredAt, &v.Payload, &v.MonitoringCycleID); err != nil {
			return nil, err
		}
		v.SourceDigest = "sha256:" + hex.EncodeToString(digest)
		out = append(out, v)
	}
	return out, rows.Err()
}
