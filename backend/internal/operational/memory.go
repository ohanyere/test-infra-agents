package operational

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

// Episode is an immutable projection of one immutable assessment revision.
// The source report is retained in full so future consumers can inspect every
// claim, observation, contract version, finding, and limitation.
type Episode struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	EnvironmentID     string    `json:"environment_id"`
	SessionID         string    `json:"session_id"`
	RunID             string    `json:"run_id"`
	ReportRevision    int       `json:"report_revision"`
	ContentDigest     string    `json:"content_digest"`
	Eligibility       string    `json:"eligibility"`
	ResourceKind      string    `json:"resource_kind"`
	ResourceNamespace string    `json:"resource_namespace"`
	ResourceName      string    `json:"resource_name"`
	ServiceName       string    `json:"service_name"`
	Operation         string    `json:"operation"`
	Report            Report    `json:"report"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
	CreatedAt         time.Time `json:"created_at"`
	Revoked           bool      `json:"revoked"`
}

type Lesson struct {
	ID                 string          `json:"id"`
	EpisodeID          string          `json:"episode_id"`
	SessionID          string          `json:"session_id,omitempty"`
	Kind               string          `json:"lesson_kind"`
	Condition          json.RawMessage `json:"condition"`
	Outcome            json.RawMessage `json:"outcome"`
	Evidence           []EvidenceRef   `json:"evidence"`
	Limitations        []string        `json:"limitations"`
	ReviewState        string          `json:"review_state"`
	SupersedesLessonID string          `json:"supersedes_lesson_id,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
}

type LessonDecision struct {
	ID        string    `json:"id"`
	LessonID  string    `json:"lesson_id"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason"`
	ActorID   string    `json:"actor_user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) ListLessonDecisions(ctx context.Context, p auth.Principal, w, e, lessonID string) ([]LessonDecision, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(lessonID) != nil {
		return nil, auth.ErrForbidden
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operational_lessons WHERE workspace_id=$1 AND environment_id=$2 AND id=$3)`, w, e, lessonID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id,lesson_id,decision,reason,actor_user_id,created_at FROM operational_lesson_decisions WHERE workspace_id=$1 AND environment_id=$2 AND lesson_id=$3 ORDER BY created_at,id`, w, e, lessonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LessonDecision{}
	for rows.Next() {
		var x LessonDecision
		if err = rows.Scan(&x.ID, &x.LessonID, &x.Decision, &x.Reason, &x.ActorID, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SupersedeLesson narrows applicability by adding a customer review note. The
// observed outcome and supporting evidence are copied byte for byte; a review
// cannot manufacture new observed facts. The successor starts pending.
func (s *Service) SupersedeLesson(ctx context.Context, p auth.Principal, w, e, id, note string) (string, error) {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return "", err
	}
	if role != "owner" && role != "admin" {
		return "", auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(id) != nil || len(strings.TrimSpace(note)) < 1 || len(note) > 1000 {
		return "", identity.ErrValidation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var episodeID, kind string
	var condition, outcome, evidence []byte
	var limits []string
	err = tx.QueryRow(ctx, `SELECT episode_id,lesson_kind,condition,outcome,evidence,limitations FROM operational_lessons WHERE workspace_id=$1 AND environment_id=$2 AND id=$3 FOR UPDATE`, w, e, id).Scan(&episodeID, &kind, &condition, &outcome, &evidence, &limits)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", auth.ErrForbidden
		}
		return "", err
	}
	var already bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operational_lessons WHERE workspace_id=$1 AND environment_id=$2 AND supersedes_lesson_id=$3)`, w, e, id).Scan(&already); err != nil {
		return "", err
	}
	if already {
		return "", identity.ErrConflict
	}
	corrected, err := correctedLessonCondition(condition, note)
	if err != nil {
		return "", err
	}
	var successor string
	err = tx.QueryRow(ctx, `INSERT INTO operational_lessons(workspace_id,environment_id,episode_id,lesson_kind,condition,outcome,evidence,limitations,supersedes_lesson_id)
	       VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8,$9) RETURNING id`, w, e, episodeID, kind, string(corrected), string(outcome), string(evidence), limits, id).Scan(&successor)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operational_lesson_decisions(lesson_id,workspace_id,environment_id,decision,reason,actor_user_id) VALUES($1,$2,$3,'retired',$4,$5)`, id, w, e, "Superseded by corrected applicability: "+strings.TrimSpace(note), p.UserID)
	if err != nil {
		return "", err
	}
	return successor, tx.Commit(ctx)
}

func correctedLessonCondition(condition []byte, note string) ([]byte, error) {
	var prior map[string]any
	if err := json.Unmarshal(condition, &prior); err != nil {
		return nil, err
	}
	prior["customer_applicability_note"] = strings.TrimSpace(note)
	return json.Marshal(prior)
}

// episodeEligibility deliberately has no shortcut from storage or an agent
// claim to verified status. Current collector-forwarded evidence fails it.
func episodeEligibility(r Report) string {
	if r.CanonicalOverallStatus == "" || r.CanonicalOverallStatus == "INCONCLUSIVE" || len(r.ObservedActions) == 0 || r.Final.Reference == "" {
		return "inconclusive"
	}
	for _, f := range r.Findings {
		if f.Status == Conflicting || f.Code == "RECONCILIATION_FAILURE" && strings.Contains(strings.ToLower(f.Reason), "rejected") {
			return "conflicting"
		}
	}
	for _, rec := range r.Reconciliations {
		if rec.State == "rejected" || rec.Result == "conflicting" {
			return "conflicting"
		}
	}
	for _, decision := range r.CustomerAttribution {
		if decision.ReasonCode == "CONFLICTING_EXECUTION_BINDINGS" {
			return "conflicting"
		}
	}
	if r.Final.SourceProvider != "kubernetes-api" || r.Initial.SourceProvider != "kubernetes-api" {
		return "provisional"
	}
	for _, limitation := range r.Limitations {
		if strings.Contains(strings.ToLower(limitation), "does not authenticate") {
			return "provisional"
		}
	}
	for _, a := range r.ObservedActions {
		if !a.OriginAuthenticated || a.Source != "kubernetes-api" {
			return "provisional"
		}
	}
	if len(r.ServiceMetricSamples) == 0 {
		return "provisional"
	}
	for _, m := range r.ServiceMetricSamples {
		if m.ProvenanceStatus != "SOURCE_AUTHENTICATED" {
			return "provisional"
		}
	}
	required := map[string]bool{"infrastructure_state": false, "service_outcome": false, "temporal_stability": false}
	for _, f := range r.Findings {
		if f.Status == Inconclusive || f.Status == Conflicting {
			return "inconclusive"
		}
		if f.ProvenanceStatus != "SOURCE_AUTHENTICATED" && f.ProvenanceStatus != "SERVER_OWNED_CONTRACTS" && f.Status != NotApplicable {
			return "provisional"
		}
		if _, ok := required[f.Dimension]; ok && (f.Status == Pass || f.Status == Fail) && f.ProvenanceStatus == "SOURCE_AUTHENTICATED" && len(f.Evidence) > 0 {
			required[f.Dimension] = true
		}
	}
	for _, supported := range required {
		if !supported {
			return "inconclusive"
		}
	}
	if r.CanonicalOverallStatus != "VERIFIED" && r.CanonicalOverallStatus != "FAILED" {
		return "inconclusive"
	}
	return "verified"
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func episodeFromReport(w, e, sessionID string, r Report) (Episode, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return Episode{}, err
	}
	x := Episode{WorkspaceID: w, EnvironmentID: e, SessionID: sessionID, RunID: r.RunID, ReportRevision: r.Revision, ContentDigest: digestBytes(data), Eligibility: episodeEligibility(r), Report: r, EvaluatedAt: r.EvaluatedAt}
	if len(r.ObservedActions) > 0 {
		a := r.ObservedActions[0]
		x.ResourceKind = a.Kind
		x.ResourceNamespace = a.Namespace
		x.ResourceName = a.Name
		x.Operation = a.Operation
	}
	if len(r.Contracts) > 0 {
		x.ServiceName = r.Contracts[0].Service
	}
	if x.ServiceName == "" && len(r.ServiceMetricSamples) > 0 {
		m := r.ServiceMetricSamples[0]
		x.ServiceName = m.Namespace + "/" + m.Service
	}
	return x, nil
}

func (s *Service) persistEpisode(ctx context.Context, tx pgx.Tx, w, e, sessionID string, r Report) error {
	x, err := episodeFromReport(w, e, sessionID, r)
	if err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operational_episodes(workspace_id,environment_id,session_id,run_id,report_revision,content_digest,eligibility,resource_kind,resource_namespace,resource_name,service_name,operation,episode,payload_bytes,evaluated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15) ON CONFLICT (workspace_id,run_id,report_revision) DO NOTHING`, w, e, sessionID, x.RunID, x.ReportRevision, x.ContentDigest, x.Eligibility, x.ResourceKind, x.ResourceNamespace, x.ResourceName, x.ServiceName, x.Operation, string(body), body, x.EvaluatedAt)
	if err != nil {
		return err
	}
	var priorDigest string
	var priorBytes []byte
	err = tx.QueryRow(ctx, `SELECT id,content_digest,payload_bytes FROM operational_episodes WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND run_id=$4 AND report_revision=$5`, w, e, sessionID, x.RunID, x.ReportRevision).Scan(&x.ID, &priorDigest, &priorBytes)
	if err != nil {
		return err
	}
	if priorDigest != x.ContentDigest || !bytes.Equal(priorBytes, body) {
		return fmt.Errorf("episode report revision has contradictory content")
	}
	if err = s.persistRelationships(ctx, tx, x); err != nil {
		return err
	}
	if x.Eligibility != "verified" {
		return nil
	}
	for _, f := range r.Findings {
		if f.Status != Fail || f.ProvenanceStatus != "SOURCE_AUTHENTICATED" || len(f.Evidence) == 0 {
			continue
		}
		condition, _ := json.Marshal(map[string]any{"resource_kind": x.ResourceKind, "namespace": x.ResourceNamespace, "resource_name": x.ResourceName, "service": x.ServiceName, "operation": x.Operation, "contract_id": f.ContractID, "contract_version": f.ContractVersion})
		outcome, _ := json.Marshal(map[string]any{"dimension": f.Dimension, "code": f.Code, "status": f.Status, "reason": f.Reason})
		evidence, _ := json.Marshal(f.Evidence)
		_, err = tx.Exec(ctx, `INSERT INTO operational_lessons(workspace_id,environment_id,episode_id,lesson_kind,condition,outcome,evidence,limitations) VALUES($1,$2,$3,'observed_failure',$4::jsonb,$5::jsonb,$6::jsonb,$7) ON CONFLICT DO NOTHING`, w, e, x.ID, string(condition), string(outcome), string(evidence), r.Limitations)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) MaterializeSession(ctx context.Context, p auth.Principal, w, e, sessionID string) ([]Episode, error) {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return nil, err
	}
	if role == "viewer" {
		return nil, auth.ErrForbidden
	}
	reports, err := s.ListReports(ctx, p, w, e, sessionID)
	if err != nil {
		return nil, err
	}
	for index, r := range reports {
		if index > 0 && monitoringOnlyGap(reports[index-1], r) {
			continue
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		err = s.persistEpisode(ctx, tx, w, e, sessionID, r)
		if err != nil {
			tx.Rollback(ctx)
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return s.listEpisodes(ctx, p, w, e, true, sessionID)
}

func (s *Service) ListEpisodes(ctx context.Context, p auth.Principal, w, e string, includeProvisional bool) ([]Episode, error) {
	return s.listEpisodes(ctx, p, w, e, includeProvisional, "")
}

func (s *Service) listEpisodes(ctx context.Context, p auth.Principal, w, e string, includeProvisional bool, sessionID string) ([]Episode, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT ep.id,ep.workspace_id,ep.environment_id,ep.session_id,ep.run_id,ep.report_revision,ep.content_digest,ep.eligibility,ep.resource_kind,ep.resource_namespace,ep.resource_name,ep.service_name,ep.operation,ep.payload_bytes,ep.evaluated_at,ep.created_at,COALESCE((SELECT d.decision='revoked' FROM operational_episode_decisions d WHERE d.episode_id=ep.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1),false) FROM operational_episodes ep WHERE ep.workspace_id=$1 AND ep.environment_id=$2 AND ($4='' OR ep.session_id=$4::uuid) AND ($3 OR (ep.eligibility='verified' AND ep.report_revision=(SELECT MAX(x.report_revision) FROM operational_episodes x WHERE x.workspace_id=ep.workspace_id AND x.run_id=ep.run_id) AND COALESCE((SELECT d.decision FROM operational_episode_decisions d WHERE d.episode_id=ep.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1),'restored')<>'revoked')) ORDER BY ep.evaluated_at DESC,ep.id DESC LIMIT 100`, w, e, includeProvisional, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Episode{}
	for rows.Next() {
		var x Episode
		var body []byte
		err = rows.Scan(&x.ID, &x.WorkspaceID, &x.EnvironmentID, &x.SessionID, &x.RunID, &x.ReportRevision, &x.ContentDigest, &x.Eligibility, &x.ResourceKind, &x.ResourceNamespace, &x.ResourceName, &x.ServiceName, &x.Operation, &body, &x.EvaluatedAt, &x.CreatedAt, &x.Revoked)
		if err != nil {
			return nil, err
		}
		if digestBytes(body) != x.ContentDigest {
			return nil, fmt.Errorf("episode %s content digest mismatch", x.ID)
		}
		if err = json.Unmarshal(body, &x.Report); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) DecideEpisode(ctx context.Context, p auth.Principal, w, e, id, decision, reason string) error {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(id) != nil || decision != "revoked" && decision != "restored" || len(reason) < 1 || len(reason) > 1000 {
		return identity.ErrValidation
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO operational_episode_decisions(workspace_id,environment_id,episode_id,decision,reason,actor_user_id) SELECT $1,$2,id,$4,$5,$6 FROM operational_episodes WHERE workspace_id=$1 AND environment_id=$2 AND id=$3`, w, e, id, decision, reason, p.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}

func (s *Service) ListLessons(ctx context.Context, p auth.Principal, w, e string) ([]Lesson, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT l.id,l.episode_id,ep.session_id,l.lesson_kind,l.condition,l.outcome,l.evidence,l.limitations,l.created_at,COALESCE(l.supersedes_lesson_id::text,''),COALESCE((SELECT d.decision FROM operational_lesson_decisions d WHERE d.lesson_id=l.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1),'pending') FROM operational_lessons l JOIN operational_episodes ep ON ep.id=l.episode_id AND ep.workspace_id=l.workspace_id AND ep.environment_id=l.environment_id WHERE l.workspace_id=$1 AND l.environment_id=$2 ORDER BY l.created_at DESC,l.id DESC LIMIT 100`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lesson{}
	for rows.Next() {
		var x Lesson
		var ev []byte
		err = rows.Scan(&x.ID, &x.EpisodeID, &x.SessionID, &x.Kind, &x.Condition, &x.Outcome, &ev, &x.Limitations, &x.CreatedAt, &x.SupersedesLessonID, &x.ReviewState)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(ev, &x.Evidence); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) ReviewLesson(ctx context.Context, p auth.Principal, w, e, id, decision, reason string) error {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(id) != nil || decision != "approved" && decision != "revoked" && decision != "rejected" && decision != "retired" || len(reason) < 1 || len(reason) > 1000 {
		return identity.ErrValidation
	}
	if decision == "approved" {
		var eligible bool
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operational_lessons l JOIN operational_episodes ep ON ep.id=l.episode_id AND ep.workspace_id=l.workspace_id AND ep.environment_id=l.environment_id WHERE l.workspace_id=$1 AND l.environment_id=$2 AND l.id=$3 AND ep.eligibility='verified' AND ep.report_revision=(SELECT MAX(x.report_revision) FROM operational_episodes x WHERE x.workspace_id=ep.workspace_id AND x.run_id=ep.run_id) AND COALESCE((SELECT d.decision FROM operational_episode_decisions d WHERE d.episode_id=ep.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1),'restored')<>'revoked' AND COALESCE((SELECT d.decision FROM operational_lesson_decisions d WHERE d.lesson_id=l.id ORDER BY d.created_at DESC,d.id DESC LIMIT 1),'pending')<>'retired' AND NOT EXISTS(SELECT 1 FROM operational_lessons successor WHERE successor.supersedes_lesson_id=l.id))`, w, e, id).Scan(&eligible)
		if err != nil {
			return err
		}
		if !eligible {
			return auth.ErrForbidden
		}
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO operational_lesson_decisions(lesson_id,workspace_id,environment_id,decision,reason,actor_user_id) SELECT id,$1,$2,$4,$5,$6 FROM operational_lessons WHERE workspace_id=$1 AND environment_id=$2 AND id=$3`, w, e, id, decision, reason, p.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrForbidden
	}
	return nil
}
