package actionattribution

import (
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

const ContractVersion = "kubernetes-action-attribution:v1"

type ResourceIdentity struct {
	UID             string    `json:"uid,omitempty"`
	ResourceVersion string    `json:"resource_version,omitempty"`
	Generation      int64     `json:"generation,omitempty"`
	Value           string    `json:"value,omitempty"`
	ObservedAt      time.Time `json:"observed_at,omitempty"`
}

type Evidence struct {
	ContractVersion      string                    `json:"contract_version"`
	RunID                string                    `json:"run_id,omitempty"`
	ActionID             string                    `json:"action_id,omitempty"`
	Executor             string                    `json:"executor"`
	ActionType           string                    `json:"action_type"`
	Target               agentgateway.ActionTarget `json:"target"`
	Field                string                    `json:"field"`
	RequestedValue       string                    `json:"requested_value"`
	AttemptedValue       string                    `json:"attempted_value,omitempty"`
	ExecutionStartedAt   time.Time                 `json:"execution_started_at"`
	ExecutionCompletedAt time.Time                 `json:"execution_completed_at,omitempty"`
	PreMutation          *ResourceIdentity         `json:"pre_mutation,omitempty"`
	PostMutation         *ResourceIdentity         `json:"post_mutation,omitempty"`
}
