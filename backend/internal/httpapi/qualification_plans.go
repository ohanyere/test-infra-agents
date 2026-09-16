package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/qualplan"
)

// QualificationPlanService is implemented by qualplan.Service. Plan builds
// and suite builds require owner/admin/member; reads require any workspace
// membership. Planning state is server-derived: the request accepts only an
// optional claimed-capability subset, so state=ready, selected task IDs,
// certified=true, coverage results, and qualification results all return
// 400 via DisallowUnknownFields.
type QualificationPlanService interface {
	BuildPlan(context.Context, auth.Principal, string, string, string, []string) (qualplan.Plan, error)
	GetPlan(context.Context, auth.Principal, string, string) (qualplan.Plan, error)
	BuildSuite(context.Context, auth.Principal, string, string) (qualplan.SuiteBuild, evaluationsuite.Suite, evaluationsuite.Version, error)
}

type qualificationPlanHandlers struct {
	service QualificationPlanService
	logger  *slog.Logger
}

// buildPlanHTTPRequest is the only caller-controlled planning input. Empty
// (or omitted capability_ids) means all declared competency claims.
type buildPlanHTTPRequest struct {
	CapabilityIDs []string `json:"capability_ids,omitempty"`
}

func (h qualificationPlanHandlers) buildPlan(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	agentID, ok := pathUUID(w, r, "agentID")
	if !ok {
		return
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	var request buildPlanHTTPRequest
	if r.ContentLength != 0 {
		if decodeJSON(w, r, &request) != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
			return
		}
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	plan, err := h.service.BuildPlan(r.Context(), principal, workspaceID, agentID, versionID, request.CapabilityIDs)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (h qualificationPlanHandlers) getPlan(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	planID, ok := pathUUID(w, r, "planID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	plan, err := h.service.GetPlan(r.Context(), principal, workspaceID, planID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h qualificationPlanHandlers) buildSuite(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	planID, ok := pathUUID(w, r, "planID")
	if !ok {
		return
	}
	// Suite contents are fully server-derived from the READY plan. Any
	// caller-supplied field (task IDs, trial counts, certified flags) is
	// rejected; only an empty document is accepted.
	if r.ContentLength != 0 {
		var empty struct{}
		if decodeJSON(w, r, &empty) != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
			return
		}
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	build, suite, version, err := h.service.BuildSuite(r.Context(), principal, workspaceID, planID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"build":         build,
		"suite":         suite,
		"suite_version": version,
	})
}

func (h qualificationPlanHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("qualification plan request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
