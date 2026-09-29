package verification

import (
	"encoding/json"
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/actionattribution"
	"github.com/evalplane/evalplane/backend/internal/evidence"
)

const AttributionContractVersion = "action-attribution:v1"

const (
	AttributionAttributed    = "ATTRIBUTED"
	AttributionNotAttributed = "NOT_ATTRIBUTED"
	AttributionInconclusive  = "INCONCLUSIVE"
	AttributionNotApplicable = "NOT_APPLICABLE"
)

func attributionRequired(metadata json.RawMessage) bool {
	var value struct {
		ActionAttribution string `json:"action_attribution"`
	}
	return json.Unmarshal(metadata, &value) == nil && value.ActionAttribution == "required"
}

func assessActionAttribution(actions []evidence.AgentAction, final Snapshot, required bool) AttributionAssessment {
	out := AttributionAssessment{ContractVersion: AttributionContractVersion, Required: required, Status: AttributionNotApplicable, Reason: "Causal action attribution was not required for this scenario.", Actions: []ActionAttribution{}}
	if !required {
		return out
	}
	out.Status = AttributionInconclusive
	out.Reason = "No attributable Kubernetes workload image action was recorded."
	for _, action := range actions {
		if action.ActionType != "set_workload_image" {
			continue
		}
		assessment := assessImageAction(action, final)
		out.Actions = append(out.Actions, assessment)
	}
	if len(out.Actions) == 0 {
		return out
	}
	out.Status = AttributionAttributed
	out.Reason = "Every required workload image transition is linked to the platform-executed mutation evidence."
	for _, action := range out.Actions {
		if action.Status == AttributionNotAttributed {
			out.Status = AttributionNotAttributed
			out.Reason = "At least one observed workload transition contradicts its platform execution evidence."
			return out
		}
		if action.Status == AttributionInconclusive {
			out.Status = AttributionInconclusive
			out.Reason = "At least one required workload action lacks complete causal attribution evidence."
		}
	}
	return out
}

func assessImageAction(action evidence.AgentAction, final Snapshot) ActionAttribution {
	out := ActionAttribution{ActionID: action.ID, Status: AttributionInconclusive, Reason: "The action lacks complete Kubernetes execution evidence."}
	if action.TargetRef != nil {
		out.Resource = "Deployment/" + *action.TargetRef
	}
	if action.Status == "failed" || action.Status == "denied" || !action.Authorized {
		out.Status = AttributionNotAttributed
		out.Reason = "The action was not successfully authorized and executed by the platform."
		return out
	}
	if action.Status != "executed" {
		return out
	}
	var requested struct {
		Type   string `json:"type"`
		Image  string `json:"image"`
		Target struct {
			Kind, Namespace, Name string
		} `json:"target"`
	}
	if json.Unmarshal(action.RequestPayload, &requested) != nil {
		return out
	}
	out.RequestedValue = requested.Image
	var result struct {
		Attribution *actionattribution.Evidence `json:"attribution"`
	}
	if json.Unmarshal(action.ResultPayload, &result) != nil || result.Attribution == nil {
		return out
	}
	e := result.Attribution
	out.Field = e.Field
	out.PreMutation = attributedState(e.PreMutation)
	out.PostMutation = attributedState(e.PostMutation)
	target := e.Target.Kind + "/" + e.Target.Namespace + "/" + e.Target.Name
	if target != "//" {
		out.Resource = target
	}
	contradiction := func(reason string) ActionAttribution {
		out.Status, out.Reason = AttributionNotAttributed, reason
		return out
	}
	if e.ContractVersion != actionattribution.ContractVersion || e.Executor != "kubernetes-client-go" || e.ActionType != "set_workload_image" || requested.Type != "set_workload_image" {
		return contradiction("The persisted executor contract or action type does not match the requested Kubernetes mutation.")
	}
	if e.RunID != action.RunID || e.ActionID != action.ID {
		return contradiction("The executor evidence identifies a different run or action.")
	}
	if action.TargetType == nil || action.TargetRef == nil || *action.TargetType != e.Target.Kind || *action.TargetRef != e.Target.Namespace+"/"+e.Target.Name || requested.Target.Kind != e.Target.Kind || requested.Target.Namespace != e.Target.Namespace || requested.Target.Name != e.Target.Name {
		return contradiction("The requested, persisted, and executed resource targets do not match.")
	}
	if requested.Image == "" || requested.Image != e.RequestedValue || requested.Image != e.AttemptedValue {
		return contradiction("The requested image does not match the exact value attempted by the executor.")
	}
	if e.PreMutation == nil || e.PostMutation == nil || e.PreMutation.UID == "" || e.PostMutation.UID == "" || e.PreMutation.ResourceVersion == "" || e.PostMutation.ResourceVersion == "" || e.PreMutation.Generation == 0 || e.PostMutation.Generation == 0 || e.PreMutation.ObservedAt.IsZero() || e.PostMutation.ObservedAt.IsZero() {
		return out
	}
	if action.StartedAt == nil || action.CompletedAt == nil {
		return out
	}
	if e.PreMutation.UID != e.PostMutation.UID {
		return contradiction("The resource UID changed during the executor mutation.")
	}
	if e.PreMutation.Value == requested.Image || e.PostMutation.Value != requested.Image {
		return contradiction("The executor old/new values do not describe the requested image transition.")
	}
	if e.PreMutation.ResourceVersion == e.PostMutation.ResourceVersion || e.PostMutation.Generation <= e.PreMutation.Generation {
		return contradiction("The executor response did not advance resourceVersion and generation consistently.")
	}
	if e.ExecutionStartedAt.IsZero() || e.ExecutionCompletedAt.IsZero() || e.PreMutation.ObservedAt.Before(e.ExecutionStartedAt) || e.PostMutation.ObservedAt.Before(e.PreMutation.ObservedAt) || e.ExecutionCompletedAt.Before(e.PostMutation.ObservedAt) {
		return contradiction("The executor timestamps are not causally ordered.")
	}
	if e.ExecutionStartedAt.Before(*action.StartedAt) || action.CompletedAt.Before(e.ExecutionCompletedAt) {
		return contradiction("The persisted action lifecycle does not contain the executor mutation interval.")
	}
	later := findWorkload(final, e.Target.Kind, e.Target.Namespace, e.Target.Name)
	if later == nil || later.UID == "" || later.ResourceVersion == "" || later.Generation == 0 || final.CapturedAt.IsZero() {
		return out
	}
	out.LaterObserved = &AttributedResourceState{UID: later.UID, ResourceVersion: later.ResourceVersion, Generation: later.Generation, Value: later.Image, ObservedAt: final.CapturedAt}
	if later.UID != e.PostMutation.UID {
		return contradiction("The later workload has a different UID and is a recreated resource, not the executor-mutated object.")
	}
	if later.Image != requested.Image {
		return contradiction("The later workload image does not match the value written by the executor.")
	}
	if later.Generation != e.PostMutation.Generation {
		return contradiction("The later workload generation proves another specification mutation occurred after the executor response.")
	}
	if final.CapturedAt.Before(e.PostMutation.ObservedAt) {
		return contradiction("The purported later observation predates the executor response.")
	}
	out.Status = AttributionAttributed
	out.Reason = fmt.Sprintf("Action %s wrote %s on the same UID and the later observation retained that generation and value.", action.ID, requested.Image)
	return out
}

func attributedState(in *actionattribution.ResourceIdentity) *AttributedResourceState {
	if in == nil {
		return nil
	}
	return &AttributedResourceState{UID: in.UID, ResourceVersion: in.ResourceVersion, Generation: in.Generation, Value: in.Value, ObservedAt: in.ObservedAt}
}

func findWorkload(snapshot Snapshot, kind, namespace, name string) *struct {
	UID, ResourceVersion, Image string
	Generation                  int64
} {
	for _, workload := range snapshot.Observation.Workloads {
		if workload.Kind == kind && workload.Namespace == namespace && workload.Name == name {
			return &struct {
				UID, ResourceVersion, Image string
				Generation                  int64
			}{workload.UID, workload.ResourceVersion, workload.Image, workload.Generation}
		}
	}
	return nil
}
