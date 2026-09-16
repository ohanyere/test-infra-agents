package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type AgentRegressionService interface {
	Create(context.Context, auth.Principal, string, agentregression.CreateInput) (agentregression.Comparison, error)
	Get(context.Context, auth.Principal, string, string) (agentregression.Comparison, error)
	Results(context.Context, auth.Principal, string, string) (agentregression.Results, error)
}

type agentRegressionHandlers struct {
	service AgentRegressionService
	logger  *slog.Logger
}

func (h agentRegressionHandlers) create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var in agentregression.CreateInput
	if decodeJSON(w, r, &in) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.Create(r.Context(), principal, workspaceID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h agentRegressionHandlers) get(w http.ResponseWriter, r *http.Request) {
	workspaceID, id, ok := regressionPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.Get(r.Context(), principal, workspaceID, id)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h agentRegressionHandlers) results(w http.ResponseWriter, r *http.Request) {
	workspaceID, id, ok := regressionPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.Results(r.Context(), principal, workspaceID, id)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func regressionPathIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	id, ok := pathUUID(w, r, "comparisonID")
	return workspaceID, id, ok
}
func (h agentRegressionHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("agent regression request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
