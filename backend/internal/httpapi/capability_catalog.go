package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/competency"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/go-chi/chi/v5"
)

// CapabilityClaimService is implemented by the agent service. Claims inherit
// AgentVersion registration permissions: any workspace member may read,
// owner/admin/member may register, viewer is read-only.
type CapabilityClaimService interface {
	ListVersionClaims(context.Context, auth.Principal, string, string, string) ([]agent.CapabilityClaim, error)
}

type capabilityCatalogHandlers struct {
	claims   CapabilityClaimService
	coverage CoverageService
	logger   *slog.Logger
}

func (h capabilityCatalogHandlers) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"contract": competency.ContractVersion,
		"items":    competency.List(),
	})
}

func (h capabilityCatalogHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "capabilityID")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "capability ID is required")
		return
	}
	item, ok := competency.Get(id)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "capability is not in the server catalog")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h capabilityCatalogHandlers) listClaims(w http.ResponseWriter, r *http.Request) {
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
	p, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.claims.ListVersionClaims(r.Context(), p, workspaceID, agentID, versionID)
	if err != nil {
		if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
			writeAuthError(w, err)
			return
		}
		if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
			h.logger.Error("capability claims request failed", "error", err)
		}
		writeDomainError(w, err)
		return
	}
	if items == nil {
		items = []agent.CapabilityClaim{}
	}
	// A claim never implies qualification. claim_status is always "claimed".
	// test_status is machine-readable (untested by default, enriched from the
	// latest persisted coverage assessment when available).
	// qualification_status is always null in V1: per-capability qualification
	// is not implemented and is never inferred from TESTED.
	if h.coverage != nil {
		if assessed, err := h.coverage.ListVersionCoverage(r.Context(), p, workspaceID, agentID, versionID); err == nil {
			byCapability := make(map[string]string, len(assessed))
			for _, item := range assessed {
				byCapability[item.CapabilityID] = item.TestStatus
			}
			for i := range items {
				if status, ok := byCapability[items[i].CapabilityID]; ok && status != "" {
					items[i].TestStatus = status
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"contract":           competency.ContractVersion,
		"claim_status":       competency.ClaimStatusClaimed,
		"items":              items,
		"qualification_note": "Claims are not qualifications. Evalplane must test them before they become qualified.",
	})
}
