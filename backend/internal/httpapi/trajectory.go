package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/trajectory"
	"github.com/go-chi/chi/v5"
)

// TrajectoryService is implemented by trajectory.Service. All endpoints are
// deterministic reads over persisted trial evidence: any workspace member
// (including viewer) may read. There are no mutation endpoints.
type TrajectoryService interface {
	GetTrajectory(context.Context, auth.Principal, string, string, trajectory.Filters) (trajectory.Trajectory, error)
	GetEvidence(context.Context, auth.Principal, string, string, string) (trajectory.EvidenceDetail, error)
	SuiteSummary(context.Context, auth.Principal, string, string) (trajectory.SuiteTrajectorySummary, error)
	Compare(context.Context, auth.Principal, string, string, string) (trajectory.ComparisonSummary, error)
}

type trajectoryHandlers struct {
	service TrajectoryService
	logger  *slog.Logger
}

func (h trajectoryHandlers) getTrajectory(w http.ResponseWriter, r *http.Request) {
	workspaceID, runID, ok := evidencePathIDs(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	pagination, ok := parsePagination(w, r)
	if !ok {
		return
	}
	filters := trajectory.Filters{
		Limit:     pagination.Limit,
		Offset:    pagination.Offset,
		Phase:     query.Get("phase"),
		EventType: query.Get("event_type"),
		Actor:     query.Get("actor"),
		Status:    query.Get("status"),
	}
	if err := trajectory.ValidateFilters(filters); err != nil {
		writeDomainError(w, err)
		return
	}
	// Deterministic comparison of two compatible trials (summary-level only).
	if with := query.Get("compare_with"); with != "" {
		if identity.ValidateUUID(with) != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "compare_with must be a valid UUID")
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		comparison, err := h.service.Compare(r.Context(), principal, workspaceID, runID, with)
		if h.handle(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, comparison)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetTrajectory(r.Context(), principal, workspaceID, runID, filters)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h trajectoryHandlers) getEvidence(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	runID, ok := pathUUID(w, r, "runID")
	if !ok {
		return
	}
	evidenceID := chi.URLParam(r, "evidenceID")
	if evidenceID == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "evidenceID is required")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetEvidence(r.Context(), principal, workspaceID, runID, evidenceID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h trajectoryHandlers) getSuiteSummary(w http.ResponseWriter, r *http.Request) {
	workspaceID, executionID, ok := executionPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.SuiteSummary(r.Context(), principal, workspaceID, executionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h trajectoryHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("trajectory request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
