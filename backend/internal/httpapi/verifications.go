package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

type VerificationService interface {
	Get(context.Context, auth.Principal, string, string) (verification.Result, error)
}
type verificationHandlers struct {
	service VerificationService
	logger  *slog.Logger
}

func (h verificationHandlers) get(w http.ResponseWriter, r *http.Request) {
	v, ok := h.load(w, r)
	if ok {
		writeJSON(w, http.StatusOK, v)
	}
}
func (h verificationHandlers) actions(w http.ResponseWriter, r *http.Request) {
	v, ok := h.load(w, r)
	if ok {
		writeJSON(w, http.StatusOK, v.ActionInspection)
	}
}
func (h verificationHandlers) snapshots(w http.ResponseWriter, r *http.Request) {
	v, ok := h.load(w, r)
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"baseline": v.Baseline, "final": v.Final, "diff": v.InfrastructureDiff})
	}
}
func (h verificationHandlers) risks(w http.ResponseWriter, r *http.Request) {
	v, ok := h.load(w, r)
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"items": v.ForwardRisks})
	}
}
func (h verificationHandlers) load(w http.ResponseWriter, r *http.Request) (verification.Result, bool) {
	workspaceID, runID, ok := evidencePathIDs(w, r)
	if !ok {
		return verification.Result{}, false
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, err := h.service.Get(r.Context(), p, workspaceID, runID)
	if err == nil {
		return v, true
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
	} else {
		if !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrValidation) {
			h.logger.Error("verification request failed", "error", err)
		}
		writeDomainError(w, err)
	}
	return verification.Result{}, false
}
