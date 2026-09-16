package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type ExecutionService interface {
	StartBatch(context.Context, auth.Principal, string, string) (execution.BatchState, error)
	StopBatch(context.Context, auth.Principal, string, string) (execution.BatchState, error)
}

type executionHandlers struct {
	service ExecutionService
	logger  *slog.Logger
}

func (h executionHandlers) startBatch(w http.ResponseWriter, r *http.Request) {
	workspaceID, batchID, ok := executionBatchPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.StartBatch(r.Context(), principal, workspaceID, batchID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h executionHandlers) stopBatch(w http.ResponseWriter, r *http.Request) {
	workspaceID, batchID, ok := executionBatchPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.StopBatch(r.Context(), principal, workspaceID, batchID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func executionBatchPathIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	batchID, ok := pathUUID(w, r, "batchID")
	if !ok {
		return "", "", false
	}
	return workspaceID, batchID, true
}

func (h executionHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("execution lifecycle request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
