package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/coverage"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/go-chi/chi/v5"
)

// CoverageService is implemented by coverage.Service. Reads require any
// workspace membership (viewer may read); writes require owner/admin/member.
// Coverage itself is deterministic server-derived state: no endpoint sets
// tested=true directly.
type CoverageService interface {
	AssignFamily(context.Context, auth.Principal, string, string, string, string, string) (coverage.Assignment, error)
	GetAssignment(context.Context, auth.Principal, string, string, string) (coverage.Assignment, error)
	ComputeExecutionCoverage(context.Context, auth.Principal, string, string, string, string) ([]coverage.Assessment, error)
	PersistExecutionCoverage(context.Context, auth.Principal, string, string, string, string) ([]coverage.PersistedAssessment, error)
	ListVersionCoverage(context.Context, auth.Principal, string, string, string) ([]coverage.PersistedAssessment, error)
	ListExecutionCoverage(context.Context, auth.Principal, string, string, string) ([]coverage.PersistedAssessment, error)
}

type coverageHandlers struct {
	service CoverageService
	logger  *slog.Logger
}

func (h coverageHandlers) taskFamilies(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"contract": coverage.TaskFamilyContractVersion,
		"items":    coverage.ListTaskFamilies(),
	})
}

func (h coverageHandlers) coverageRequirements(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"contract": coverage.CoverageContractVersion,
		"items":    coverage.ListRequirements(),
	})
}

type assignFamilyRequest struct {
	FamilyID string `json:"family_id"`
	CaseType string `json:"case_type"`
}

func (h coverageHandlers) assignFamily(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	scenarioID, ok := pathUUID(w, r, "scenarioID")
	if !ok {
		return
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	var request assignFamilyRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.AssignFamily(r.Context(), principal, workspaceID, scenarioID, versionID, request.FamilyID, request.CaseType)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h coverageHandlers) getAssignment(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	scenarioID, ok := pathUUID(w, r, "scenarioID")
	if !ok {
		return
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetAssignment(r.Context(), principal, workspaceID, scenarioID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h coverageHandlers) versionCoverage(w http.ResponseWriter, r *http.Request) {
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
	principal, _ := auth.PrincipalFromContext(r.Context())
	values, err := h.service.ListVersionCoverage(r.Context(), principal, workspaceID, agentID, versionID)
	if h.handle(w, err) {
		return
	}
	if values == nil {
		values = []coverage.PersistedAssessment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"coverage_contract_version":    coverage.CoverageContractVersion,
		"task_family_contract_version": coverage.TaskFamilyContractVersion,
		"capability_catalog_version":   coverage.CapabilityCatalogVersion,
		"agent_version_id":             versionID,
		"items":                        values,
	})
}

func (h coverageHandlers) executionCoverage(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	executionID, ok := pathUUID(w, r, "executionID")
	if !ok {
		return
	}
	agentVersionID := chi.URLParam(r, "agentVersionID")
	if agentVersionID == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "agent version ID is required")
		return
	}
	if identity.ValidateUUID(agentVersionID) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "agent version ID must be a valid UUID")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	values, err := h.service.ComputeExecutionCoverage(r.Context(), principal, workspaceID, "", agentVersionID, executionID)
	if h.handle(w, err) {
		return
	}
	if values == nil {
		values = []coverage.Assessment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"coverage_contract_version":    coverage.CoverageContractVersion,
		"task_family_contract_version": coverage.TaskFamilyContractVersion,
		"capability_catalog_version":   coverage.CapabilityCatalogVersion,
		"suite_execution_id":           executionID,
		"agent_version_id":             agentVersionID,
		"items":                        values,
	})
}

func (h coverageHandlers) persistExecutionCoverage(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	executionID, ok := pathUUID(w, r, "executionID")
	if !ok {
		return
	}
	agentVersionID := chi.URLParam(r, "agentVersionID")
	if agentVersionID == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "agent version ID is required")
		return
	}
	if identity.ValidateUUID(agentVersionID) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "agent version ID must be a valid UUID")
		return
	}
	var request struct {
		AgentID string `json:"agent_id"`
	}
	if r.ContentLength != 0 {
		if decodeJSON(w, r, &request) != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
			return
		}
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	values, err := h.service.PersistExecutionCoverage(r.Context(), principal, workspaceID, request.AgentID, agentVersionID, executionID)
	if h.handle(w, err) {
		return
	}
	if values == nil {
		values = []coverage.PersistedAssessment{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"coverage_contract_version":    coverage.CoverageContractVersion,
		"task_family_contract_version": coverage.TaskFamilyContractVersion,
		"capability_catalog_version":   coverage.CapabilityCatalogVersion,
		"suite_execution_id":           executionID,
		"agent_version_id":             agentVersionID,
		"items":                        values,
	})
}

func (h coverageHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("capability coverage request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}

// agentClaimsForCoverage adapts agent claims to the coverage ClaimReader.
type agentClaimsForCoverage struct {
	claims interface {
		ListVersionClaims(context.Context, auth.Principal, string, string, string) ([]agent.CapabilityClaim, error)
	}
}

func (a agentClaimsForCoverage) ListVersionClaims(ctx context.Context, principal auth.Principal, workspaceID, agentID, versionID string) ([]coverage.ClaimSummary, error) {
	items, err := a.claims.ListVersionClaims(ctx, principal, workspaceID, agentID, versionID)
	if err != nil {
		return nil, err
	}
	out := make([]coverage.ClaimSummary, 0, len(items))
	for _, item := range items {
		out = append(out, coverage.ClaimSummary{CapabilityID: item.CapabilityID})
	}
	return out, nil
}
