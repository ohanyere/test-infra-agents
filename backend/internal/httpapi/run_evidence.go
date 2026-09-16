package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type EvidenceService interface {
	ListRunEvents(context.Context, auth.Principal, string, string, evidence.EventFilters) ([]evidence.RunEvent, error)
	ListAgentActions(context.Context, auth.Principal, string, string, evidence.ActionFilters) ([]evidence.AgentAction, error)
	GetEvaluation(context.Context, auth.Principal, string, string) (evidence.Evaluation, error)
}

type evidenceHandlers struct {
	service EvidenceService
	logger  *slog.Logger
}

func (h evidenceHandlers) listEvents(w http.ResponseWriter, r *http.Request) {
	workspaceID, runID, ok := evidencePathIDs(w, r)
	if !ok {
		return
	}
	pagination, ok := parsePagination(w, r)
	if !ok {
		return
	}
	filters, err := evidence.ValidateEventFilters(evidence.EventFilters{Pagination: pagination, EventType: r.URL.Query().Get("event_type")})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListRunEvents(r.Context(), principal, workspaceID, runID, filters)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[evidence.RunEvent]{Items: items, Pagination: paginationResponse{Limit: pagination.Limit, Offset: pagination.Offset}})
}

func (h evidenceHandlers) listActions(w http.ResponseWriter, r *http.Request) {
	workspaceID, runID, ok := evidencePathIDs(w, r)
	if !ok {
		return
	}
	pagination, ok := parsePagination(w, r)
	if !ok {
		return
	}
	filters, err := evidence.ValidateActionFilters(evidence.ActionFilters{Pagination: pagination, Status: r.URL.Query().Get("status")})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListAgentActions(r.Context(), principal, workspaceID, runID, filters)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[evidence.AgentAction]{Items: items, Pagination: paginationResponse{Limit: pagination.Limit, Offset: pagination.Offset}})
}

func (h evidenceHandlers) getEvaluation(w http.ResponseWriter, r *http.Request) {
	workspaceID, runID, ok := evidencePathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetEvaluation(r.Context(), principal, workspaceID, runID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func evidencePathIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	runID, ok := pathUUID(w, r, "runID")
	if !ok {
		return "", "", false
	}
	return workspaceID, runID, true
}

func (h evidenceHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("run evidence request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
