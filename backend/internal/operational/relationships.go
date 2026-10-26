package operational

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

type EpisodeRelationship struct {
	EpisodeID       string    `json:"episode_id"`
	Ordinal         int       `json:"ordinal"`
	FromKind        string    `json:"from_kind"`
	FromRef         string    `json:"from_ref"`
	Relation        string    `json:"relation"`
	ToKind          string    `json:"to_kind"`
	ToRef           string    `json:"to_ref"`
	Observed        bool      `json:"observed"`
	Provenance      string    `json:"provenance"`
	SourceReference string    `json:"source_reference"`
	OccurredAt      time.Time `json:"occurred_at"`
}

func episodeRelationships(x Episode) []EpisodeRelationship {
	out := []EpisodeRelationship{}
	appendEdge := func(fromKind, fromRef, relation, toKind, toRef string, observed bool, provenance, source string, at time.Time) {
		if fromRef == "" || toRef == "" {
			return
		}
		if at.IsZero() {
			at = x.EvaluatedAt
		}
		out = append(out, EpisodeRelationship{EpisodeID: x.ID, Ordinal: len(out), FromKind: fromKind, FromRef: fromRef, Relation: relation, ToKind: toKind, ToRef: toRef, Observed: observed, Provenance: provenance, SourceReference: source, OccurredAt: at})
	}
	for _, a := range x.Report.ObservedActions {
		resource := strings.Trim(a.Kind+"/"+a.Namespace+"/"+a.Name, "/")
		appendEdge("episode", x.ID, "contains_action", "action", a.ID, a.OriginAuthenticated, a.Source, a.ID, a.OccurredAt)
		appendEdge("action", a.ID, "affects", "resource", resource, a.OriginAuthenticated, a.Source, a.ID, a.OccurredAt)
		if x.ServiceName != "" {
			appendEdge("resource", resource, "associated_service", "service", x.ServiceName, false, "report_scope", a.ID, x.EvaluatedAt)
		}
	}
	for _, c := range x.Report.Contracts {
		appendEdge("episode", x.ID, "evaluated_against", "contract", c.ID+"/v"+strconv.Itoa(c.Version), false, "server_owned_contract", c.ID, x.EvaluatedAt)
	}
	for _, f := range x.Report.Findings {
		appendEdge("episode", x.ID, "has_outcome", "outcome", f.Dimension+"/"+f.Status, x.Eligibility == "verified" && f.ProvenanceStatus == "SOURCE_AUTHENTICATED", f.ProvenanceStatus, f.Code, x.EvaluatedAt)
		for _, ref := range f.Evidence {
			appendEdge("episode", x.ID, "supported_by", "evidence", ref.Reference, ref.Provenance == "SOURCE_AUTHENTICATED" || ref.Provenance == "kubernetes-api", ref.Provenance, ref.Reference, x.EvaluatedAt)
		}
	}
	return out
}
func (s *Service) persistRelationships(ctx context.Context, tx pgx.Tx, x Episode) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operational_episode_relationships WHERE episode_id=$1)`, x.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	edges := episodeRelationships(x)
	if len(edges) > 2000 {
		return fmt.Errorf("episode relationship count exceeds bounded limit")
	}
	for _, edge := range edges {
		_, err := tx.Exec(ctx, `INSERT INTO operational_episode_relationships(episode_id,ordinal,workspace_id,environment_id,from_kind,from_ref,relation,to_kind,to_ref,observed,provenance,source_reference,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, x.ID, edge.Ordinal, x.WorkspaceID, x.EnvironmentID, edge.FromKind, edge.FromRef, edge.Relation, edge.ToKind, edge.ToRef, edge.Observed, edge.Provenance, edge.SourceReference, edge.OccurredAt)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ListEpisodeRelationships(ctx context.Context, p auth.Principal, w, e, id string) ([]EpisodeRelationship, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(id) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT r.episode_id,r.ordinal,r.from_kind,r.from_ref,r.relation,r.to_kind,r.to_ref,r.observed,r.provenance,r.source_reference,r.occurred_at FROM operational_episode_relationships r JOIN operational_episodes ep ON ep.id=r.episode_id WHERE ep.workspace_id=$1 AND ep.environment_id=$2 AND ep.id=$3 ORDER BY r.ordinal`, w, e, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EpisodeRelationship{}
	for rows.Next() {
		var x EpisodeRelationship
		if err = rows.Scan(&x.EpisodeID, &x.Ordinal, &x.FromKind, &x.FromRef, &x.Relation, &x.ToKind, &x.ToRef, &x.Observed, &x.Provenance, &x.SourceReference, &x.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
