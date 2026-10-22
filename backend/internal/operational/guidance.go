package operational

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type GuidanceCredentialInput struct {
	AgentVersionID string    `json:"agent_version_id"`
	Namespaces     []string  `json:"namespaces"`
	ResourceKinds  []string  `json:"resource_kinds"`
	ExpiresAt      time.Time `json:"expires_at"`
}
type GuidanceCredential struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	EnvironmentID  string     `json:"environment_id"`
	AgentVersionID string     `json:"agent_version_id"`
	Token          string     `json:"token,omitempty"` // shown only at issuance
	Namespaces     []string   `json:"namespaces"`
	ResourceKinds  []string   `json:"resource_kinds"`
	ExpiresAt      time.Time  `json:"expires_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}
type ProposedOperation struct {
	RunID        string `json:"run_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Namespace    string `json:"namespace"`
	ResourceKind string `json:"resource_kind"`
	ResourceName string `json:"resource_name"`
	Operation    string `json:"operation"`
	Service      string `json:"service,omitempty"`
	Image        string `json:"image,omitempty"`
}
type RelevantEpisode struct {
	ID                string        `json:"id"`
	ReportRevision    int           `json:"report_revision"`
	ContentDigest     string        `json:"content_digest"`
	Resource          string        `json:"resource"`
	Operation         string        `json:"operation"`
	CanonicalStatus   string        `json:"canonical_status"`
	AttributionStatus string        `json:"attribution_status"`
	Findings          []Finding     `json:"findings"`
	Evidence          []EvidenceRef `json:"evidence"`
	EvaluatedAt       time.Time     `json:"evaluated_at"`
	RelevanceScore    int           `json:"relevance_score"`
}
type Guidance struct {
	ID                          string            `json:"id"`
	WorkspaceID                 string            `json:"workspace_id"`
	EnvironmentID               string            `json:"environment_id"`
	AgentVersionID              string            `json:"agent_version_id,omitempty"`
	ProposedOperation           ProposedOperation `json:"proposed_operation"`
	Requirements                []Contract        `json:"requirements"`
	Episodes                    []RelevantEpisode `json:"episodes"`
	Lessons                     []Lesson          `json:"lessons"`
	TrustworthyHistoryAvailable bool              `json:"trustworthy_history_available"`
	Limitations                 []string          `json:"limitations"`
	CreatedAt                   time.Time         `json:"created_at"`
}

func validOperation(op ProposedOperation) bool {
	if (op.RunID != "" && identity.ValidateUUID(op.RunID) != nil) || (op.SessionID != "" && identity.ValidateUUID(op.SessionID) != nil) {
		return false
	}
	if op.Namespace == "" || op.ResourceName == "" || op.ResourceKind != "Deployment" || len(op.Namespace) > 120 || len(op.ResourceName) > 120 || len(op.Service) > 240 || len(op.Image) > 512 {
		return false
	}
	switch op.Operation {
	case "create", "patch", "update", "delete", "scale", "set_image":
		return true
	}
	return false
}
func subset(requested, allowed []string) bool {
	if len(requested) == 0 {
		return false
	}
	for _, v := range requested {
		if v == "" || !contains(allowed, v) {
			return false
		}
	}
	return true
}
func contains(items []string, value string) bool {
	for _, x := range items {
		if x == value {
			return true
		}
	}
	return false
}
func (s *Service) IssueGuidanceCredential(ctx context.Context, p auth.Principal, w, e string, in GuidanceCredentialInput) (GuidanceCredential, error) {
	var out GuidanceCredential
	role, err := s.role(ctx, p, w)
	if err != nil {
		return out, err
	}
	if role != "owner" && role != "admin" {
		return out, auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(in.AgentVersionID) != nil || in.ExpiresAt.Before(s.now().Add(time.Minute)) || in.ExpiresAt.After(s.now().Add(90*24*time.Hour)) {
		return out, identity.ErrValidation
	}
	var namespaces, kinds []string
	err = s.pool.QueryRow(ctx, `SELECT namespaces,resource_kinds FROM customer_kubernetes_environments WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL`, w, e).Scan(&namespaces, &kinds)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, auth.ErrForbidden
	}
	if err != nil {
		return out, err
	}
	if !subset(in.Namespaces, namespaces) || !subset(in.ResourceKinds, kinds) {
		return out, auth.ErrForbidden
	}
	var exists bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_versions v JOIN agents a ON a.id=v.agent_id WHERE v.id=$1 AND a.owner_workspace_id=$2 AND a.status='active')`, in.AgentVersionID, w).Scan(&exists)
	if err != nil {
		return out, err
	}
	if !exists {
		return out, auth.ErrForbidden
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return out, err
	}
	token := "rvg_" + base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	out = GuidanceCredential{WorkspaceID: w, EnvironmentID: e, AgentVersionID: in.AgentVersionID, Token: token, Namespaces: in.Namespaces, ResourceKinds: in.ResourceKinds, ExpiresAt: in.ExpiresAt.UTC()}
	err = s.pool.QueryRow(ctx, `INSERT INTO operational_guidance_credentials(workspace_id,environment_id,agent_version_id,token_hash,namespaces,resource_kinds,expires_at,issued_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, w, e, in.AgentVersionID, hash[:], in.Namespaces, in.ResourceKinds, out.ExpiresAt, p.UserID).Scan(&out.ID)
	return out, err
}
func (s *Service) RevokeGuidanceCredential(ctx context.Context, p auth.Principal, w, e, id string) error {
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
	tag, err := s.pool.Exec(ctx, `UPDATE operational_guidance_credentials SET revoked_at=now() WHERE id=$1 AND workspace_id=$2 AND environment_id=$3 AND revoked_at IS NULL`, id, w, e)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}
func (s *Service) GuidanceForAgent(ctx context.Context, token string, op ProposedOperation) (Guidance, error) {
	var zero Guidance
	if !strings.HasPrefix(token, "rvg_") || len(token) != 47 || !validOperation(op) {
		return zero, auth.ErrForbidden
	}
	hash := sha256.Sum256([]byte(token))
	var w, e, credentialID, versionID string
	var namespaces, kinds, currentNamespaces, currentKinds []string
	err := s.pool.QueryRow(ctx, `SELECT c.id,c.workspace_id,c.environment_id,c.agent_version_id,c.namespaces,c.resource_kinds,env.namespaces,env.resource_kinds FROM operational_guidance_credentials c JOIN customer_kubernetes_environments env ON env.workspace_id=c.workspace_id AND env.id=c.environment_id JOIN agent_versions v ON v.id=c.agent_version_id JOIN agents a ON a.id=v.agent_id AND a.owner_workspace_id=c.workspace_id WHERE c.token_hash=$1 AND c.revoked_at IS NULL AND c.expires_at>now() AND env.revoked_at IS NULL AND a.status='active'`, hash[:]).Scan(&credentialID, &w, &e, &versionID, &namespaces, &kinds, &currentNamespaces, &currentKinds)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, auth.ErrUnauthenticated
	}
	if err != nil {
		return zero, err
	}
	if !contains(namespaces, op.Namespace) || !contains(kinds, op.ResourceKind) || !contains(currentNamespaces, op.Namespace) || !contains(currentKinds, op.ResourceKind) {
		return zero, auth.ErrForbidden
	}
	return s.buildGuidance(ctx, w, e, credentialID, "", versionID, op)
}
func (s *Service) PreviewGuidance(ctx context.Context, p auth.Principal, w, e string, op ProposedOperation) (Guidance, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return Guidance{}, err
	}
	if identity.ValidateUUID(e) != nil || !validOperation(op) {
		return Guidance{}, identity.ErrValidation
	}
	var namespaces, kinds []string
	err := s.pool.QueryRow(ctx, `SELECT namespaces,resource_kinds FROM customer_kubernetes_environments WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL`, w, e).Scan(&namespaces, &kinds)
	if errors.Is(err, pgx.ErrNoRows) {
		return Guidance{}, auth.ErrForbidden
	}
	if err != nil {
		return Guidance{}, err
	}
	if !contains(namespaces, op.Namespace) || !contains(kinds, op.ResourceKind) {
		return Guidance{}, auth.ErrForbidden
	}
	return s.buildGuidance(ctx, w, e, "", p.UserID, "", op)
}
func (s *Service) buildGuidance(ctx context.Context, w, e, credentialID, userID, versionID string, op ProposedOperation) (Guidance, error) {
	out := Guidance{ID: uuid.NewString(), WorkspaceID: w, EnvironmentID: e, AgentVersionID: versionID, ProposedOperation: op, Requirements: []Contract{}, Episodes: []RelevantEpisode{}, Lessons: []Lesson{}, Limitations: []string{"Historical similarity does not authorize an operation or prove a future outcome.", "Guidance does not establish that the requesting agent caused any past action."}, CreatedAt: s.now().UTC()}
	history, err := s.listContractsInternal(ctx, w, e)
	if err != nil {
		return Guidance{}, err
	}
	// The proposed service is caller supplied. Return every active environment
	// contract so an agent cannot hide a requirement by omitting that field.
	out.Requirements = activeContracts(history, out.CreatedAt)
	rows, err := s.pool.Query(ctx, `SELECT ep.id,ep.report_revision,ep.content_digest,ep.resource_kind,ep.resource_namespace,ep.resource_name,ep.service_name,ep.operation,ep.payload_bytes,ep.evaluated_at FROM operational_episodes ep WHERE ep.workspace_id=$1 AND ep.environment_id=$2 AND ep.eligibility='verified' AND ep.resource_kind=$3 AND ep.resource_namespace=$4 AND ep.evaluated_at>now()-interval '90 days' AND ep.report_revision=(SELECT MAX(x.report_revision) FROM operational_episodes x WHERE x.workspace_id=ep.workspace_id AND x.run_id=ep.run_id) AND COALESCE((SELECT d.decision FROM operational_episode_decisions d WHERE d.episode_id=ep.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1),'restored')<>'revoked' ORDER BY ep.evaluated_at DESC LIMIT 100`, w, e, op.ResourceKind, op.Namespace)
	if err != nil {
		return Guidance{}, err
	}
	for rows.Next() {
		var id, digest, kind, ns, name, service, operation string
		var revision int
		var body []byte
		var at time.Time
		if err = rows.Scan(&id, &revision, &digest, &kind, &ns, &name, &service, &operation, &body, &at); err != nil {
			rows.Close()
			return Guidance{}, err
		}
		if digestBytes(body) != digest {
			rows.Close()
			return Guidance{}, errors.New("episode content digest mismatch")
		}
		var report Report
		if err = json.Unmarshal(body, &report); err != nil {
			rows.Close()
			return Guidance{}, err
		}
		score := rankEpisode(op, kind, ns, name, service, operation, at, out.CreatedAt)
		if score <= 0 {
			continue
		}
		if op.Image != "" {
			requested, _ := json.Marshal(report.RequestedActions)
			if strings.Contains(string(requested), op.Image) {
				score += 20
			}
		}
		for _, finding := range report.Findings {
			if finding.Status == Fail && finding.ProvenanceStatus == "SOURCE_AUTHENTICATED" {
				score += 10
				break
			}
		}
		for _, prior := range report.Contracts {
			for _, active := range out.Requirements {
				if prior.ID == active.ID && prior.Version == active.Version {
					score += 15
					break
				}
			}
		}
		refs := []EvidenceRef{}
		for _, f := range report.Findings {
			refs = append(refs, f.Evidence...)
		}
		out.Episodes = append(out.Episodes, RelevantEpisode{ID: id, ReportRevision: revision, ContentDigest: digest, Resource: ns + "/" + name, Operation: operation, CanonicalStatus: report.CanonicalOverallStatus, AttributionStatus: report.Attribution.Status, Findings: report.Findings, Evidence: refs, EvaluatedAt: at, RelevanceScore: score})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Guidance{}, err
	}
	sort.Slice(out.Episodes, func(i, j int) bool {
		if out.Episodes[i].RelevanceScore == out.Episodes[j].RelevanceScore {
			return out.Episodes[i].EvaluatedAt.After(out.Episodes[j].EvaluatedAt)
		}
		return out.Episodes[i].RelevanceScore > out.Episodes[j].RelevanceScore
	})
	if len(out.Episodes) > 10 {
		out.Episodes = out.Episodes[:10]
	}
	out.TrustworthyHistoryAvailable = len(out.Episodes) > 0
	if !out.TrustworthyHistoryAvailable {
		out.Limitations = append(out.Limitations, "No eligible, recent operational episode matches this scope. Provisional history is excluded.")
	}
	ids := []string{}
	for _, x := range out.Episodes {
		ids = append(ids, x.ID)
	}
	if len(ids) > 0 {
		lrows, err := s.pool.Query(ctx, `SELECT l.id,l.episode_id,l.lesson_kind,l.condition,l.outcome,l.evidence,l.limitations,l.created_at FROM operational_lessons l WHERE l.workspace_id=$1 AND l.environment_id=$2 AND l.episode_id=ANY($3::uuid[]) AND (SELECT d.decision FROM operational_lesson_decisions d WHERE d.lesson_id=l.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1)='approved' AND NOT EXISTS(SELECT 1 FROM operational_lessons successor WHERE successor.supersedes_lesson_id=l.id) ORDER BY l.created_at DESC LIMIT 20`, w, e, ids)
		if err != nil {
			return Guidance{}, err
		}
		for lrows.Next() {
			var x Lesson
			var ev []byte
			if err = lrows.Scan(&x.ID, &x.EpisodeID, &x.Kind, &x.Condition, &x.Outcome, &ev, &x.Limitations, &x.CreatedAt); err != nil {
				lrows.Close()
				return Guidance{}, err
			}
			if err = json.Unmarshal(ev, &x.Evidence); err != nil {
				lrows.Close()
				return Guidance{}, err
			}
			x.ReviewState = "approved"
			out.Lessons = append(out.Lessons, x)
		}
		err = lrows.Err()
		lrows.Close()
		if err != nil {
			return Guidance{}, err
		}
	}
	req, _ := json.Marshal(op)
	response, _ := json.Marshal(out)
	digest := sha256.Sum256(response)
	_, err = s.pool.Exec(ctx, `INSERT INTO operational_guidance_requests(id,workspace_id,environment_id,credential_id,requested_by_user_id,request,response,response_bytes,response_digest) VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6::jsonb,$7::jsonb,$8,$9)`, out.ID, w, e, credentialID, userID, string(req), string(response), response, "sha256:"+hex.EncodeToString(digest[:]))
	return out, err
}
func rankEpisode(op ProposedOperation, kind, ns, name, service, operation string, at, now time.Time) int {
	if kind != op.ResourceKind || ns != op.Namespace || at.After(now.Add(time.Minute)) || now.Sub(at) > 90*24*time.Hour {
		return 0
	}
	if name != op.ResourceName && (op.Service == "" || service != op.Service) {
		return 0
	}
	score := 20
	if name == op.ResourceName {
		score += 100
	}
	if op.Service != "" && service == op.Service {
		score += 50
	}
	if operation == op.Operation {
		score += 30
	}
	score += int(30 - now.Sub(at).Hours()/72)
	if score < 1 {
		return 1
	}
	return score
}

func (s *Service) ListGuidanceHistory(ctx context.Context, p auth.Principal, w, e string) ([]Guidance, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT response_bytes,response_digest FROM operational_guidance_requests WHERE workspace_id=$1 AND environment_id=$2 ORDER BY created_at DESC LIMIT 100`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Guidance{}
	for rows.Next() {
		var data []byte
		var storedDigest string
		var x Guidance
		if err = rows.Scan(&data, &storedDigest); err != nil {
			return nil, err
		}
		if digestBytes(data) != storedDigest {
			return nil, errors.New("guidance response digest mismatch")
		}
		if err = json.Unmarshal(data, &x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) ListGuidanceCredentials(ctx context.Context, p auth.Principal, w, e string) ([]GuidanceCredential, error) {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return nil, err
	}
	if role != "owner" && role != "admin" {
		return nil, auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,environment_id,agent_version_id,namespaces,resource_kinds,expires_at,revoked_at FROM operational_guidance_credentials WHERE workspace_id=$1 AND environment_id=$2 ORDER BY created_at DESC LIMIT 100`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GuidanceCredential{}
	for rows.Next() {
		var x GuidanceCredential
		if err = rows.Scan(&x.ID, &x.WorkspaceID, &x.EnvironmentID, &x.AgentVersionID, &x.Namespaces, &x.ResourceKinds, &x.ExpiresAt, &x.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
