package operational

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type ExperimentRecord struct {
	ID  string        `json:"id"`
	Arm ExperimentArm `json:"arm"`
}
type ExperimentPair struct {
	DatasetID         string                `json:"dataset_id"`
	AgentVersionID    string                `json:"agent_version_id"`
	ScenarioVersionID string                `json:"scenario_version_id"`
	ExecutionKind     string                `json:"execution_kind"`
	Without           *ExperimentRecord     `json:"without_guidance,omitempty"`
	With              *ExperimentRecord     `json:"with_guidance,omitempty"`
	Comparison        *ExperimentComparison `json:"comparison,omitempty"`
	State             string                `json:"state"`
}

func validateExperimentArm(a ExperimentArm) error {
	if len(a.DatasetID) < 1 || len(a.DatasetID) > 120 || strings.ContainsAny(a.DatasetID, "\x00\n\r") || identity.ValidateUUID(a.AgentVersionID) != nil || identity.ValidateUUID(a.ScenarioVersionID) != nil || len(a.Cases) == 0 || len(a.Cases) > 1000 || a.ExecutionKind != "offline_replay" && a.ExecutionKind != "hosted" || a.Mode != "with_guidance" && a.Mode != "without_guidance" {
		return identity.ErrValidation
	}
	seen := map[string]bool{}
	for _, c := range a.Cases {
		if c.ID == "" || len(c.ID) > 120 || seen[c.ID] || len(c.Evidence) == 0 || c.ForbiddenAttempts < 0 || c.ContractViolations < 0 {
			return identity.ErrValidation
		}
		seen[c.ID] = true
	}
	return nil
}
func (s *Service) RecordGuidanceExperiment(ctx context.Context, p auth.Principal, w string, a ExperimentArm) (ExperimentRecord, error) {
	var out ExperimentRecord
	role, err := s.role(ctx, p, w)
	if err != nil {
		return out, err
	}
	if role != "owner" && role != "admin" {
		return out, auth.ErrForbidden
	}
	if err = validateExperimentArm(a); err != nil {
		return out, err
	}
	var owns bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_versions v JOIN agents ag ON ag.id=v.agent_id JOIN scenario_versions sv ON sv.id=$3 JOIN scenarios sc ON sc.id=sv.scenario_id WHERE v.id=$1 AND ag.owner_workspace_id=$2 AND (sc.owner_workspace_id=$2 OR sc.owner_workspace_id IS NULL))`, a.AgentVersionID, w, a.ScenarioVersionID).Scan(&owns)
	if err != nil {
		return out, err
	}
	if !owns {
		return out, auth.ErrForbidden
	}
	body, err := json.Marshal(a)
	if err != nil {
		return out, err
	}
	refs := []EvidenceRef{}
	for _, c := range a.Cases {
		refs = append(refs, c.Evidence...)
	}
	evidence, err := json.Marshal(refs)
	if err != nil {
		return out, err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO operational_guidance_experiments(workspace_id,dataset_id,agent_version_id,scenario_version_id,execution_kind,mode,results,evidence) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb) ON CONFLICT DO NOTHING`, w, a.DatasetID, a.AgentVersionID, a.ScenarioVersionID, a.ExecutionKind, a.Mode, string(body), string(evidence))
	if err != nil {
		return out, err
	}
	var stored []byte
	err = s.pool.QueryRow(ctx, `SELECT id,results FROM operational_guidance_experiments WHERE workspace_id=$1 AND dataset_id=$2 AND agent_version_id=$3 AND scenario_version_id=$4 AND execution_kind=$5 AND mode=$6`, w, a.DatasetID, a.AgentVersionID, a.ScenarioVersionID, a.ExecutionKind, a.Mode).Scan(&out.ID, &stored)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(stored, &out.Arm); err != nil {
		return ExperimentRecord{}, err
	}
	if !reflect.DeepEqual(out.Arm, a) {
		return ExperimentRecord{}, errors.New("experiment arm already exists with different results")
	}
	return out, nil
}
func (s *Service) ListGuidanceExperiments(ctx context.Context, p auth.Principal, w string) ([]ExperimentPair, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,results FROM operational_guidance_experiments WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 200`, w)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pairs := map[string]*ExperimentPair{}
	for rows.Next() {
		var rec ExperimentRecord
		var body []byte
		if err = rows.Scan(&rec.ID, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &rec.Arm); err != nil {
			return nil, err
		}
		a := rec.Arm
		key := a.DatasetID + "/" + a.AgentVersionID + "/" + a.ScenarioVersionID + "/" + a.ExecutionKind
		pair := pairs[key]
		if pair == nil {
			pair = &ExperimentPair{DatasetID: a.DatasetID, AgentVersionID: a.AgentVersionID, ScenarioVersionID: a.ScenarioVersionID, ExecutionKind: a.ExecutionKind, State: "awaiting_pair"}
			pairs[key] = pair
		}
		if a.Mode == "with_guidance" {
			pair.With = &rec
		} else {
			pair.Without = &rec
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []ExperimentPair{}
	for _, pair := range pairs {
		if pair.With != nil && pair.Without != nil {
			comparison, e := CompareGuidanceArms(pair.Without.Arm, pair.With.Arm)
			if e != nil {
				pair.State = "invalid_pair"
			} else {
				pair.Comparison = &comparison
				pair.State = "compared_unvalidated"
			}
		}
		out = append(out, *pair)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DatasetID == out[j].DatasetID {
			return out[i].AgentVersionID < out[j].AgentVersionID
		}
		return out[i].DatasetID < out[j].DatasetID
	})
	return out, nil
}
