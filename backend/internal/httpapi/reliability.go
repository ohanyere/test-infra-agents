package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/reliability"
)

// ReliabilityService is implemented by reliability.Service. All endpoints
// are deterministic reads over persisted trial evidence: any workspace
// member (including viewer) may read. There are no mutation endpoints for
// calculated scores; request bodies are never accepted.
type ReliabilityService interface {
	ScenarioReliability(context.Context, auth.Principal, string, string, string, string) (reliability.ScenarioReport, error)
	ExecutionReliability(context.Context, auth.Principal, string, string, string, string) (reliability.ExecutionReport, reliability.ComparisonReport, error)
	AgentVersionReliability(context.Context, auth.Principal, string, string, string) (reliability.AgentReport, error)
}

type reliabilityHandlers struct {
	service ReliabilityService
	logger  *slog.Logger
}

func (h reliabilityHandlers) agentVersionReliability(w http.ResponseWriter, r *http.Request) {
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
	value, err := h.service.AgentVersionReliability(r.Context(), principal, workspaceID, agentID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h reliabilityHandlers) executionReliability(w http.ResponseWriter, r *http.Request) {
	workspaceID, executionID, ok := executionPathIDs(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	baseline := query.Get("baseline_agent_version_id")
	candidate := query.Get("candidate_agent_version_id")
	if (baseline == "") != (candidate == "") {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "baseline_agent_version_id and candidate_agent_version_id must be provided together")
		return
	}
	for _, value := range []string{baseline, candidate} {
		if value != "" && identity.ValidateUUID(value) != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "agent version IDs must be valid UUIDs")
			return
		}
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	report, comparison, err := h.service.ExecutionReliability(r.Context(), principal, workspaceID, executionID, baseline, candidate)
	if h.handle(w, err) {
		return
	}
	if baseline == "" {
		writeJSON(w, http.StatusOK, report)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report, "comparison": comparison})
}

func (h reliabilityHandlers) scenarioVersionReliability(w http.ResponseWriter, r *http.Request) {
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
	agentVersionID := r.URL.Query().Get("agent_version_id")
	if agentVersionID != "" && identity.ValidateUUID(agentVersionID) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "agent_version_id must be a valid UUID")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.ScenarioReliability(r.Context(), principal, workspaceID, scenarioID, versionID, agentVersionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h reliabilityHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("reliability request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
