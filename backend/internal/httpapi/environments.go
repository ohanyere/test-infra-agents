package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

// EnvironmentService is implemented by environment.Service. Catalog reads
// allow viewer; requirements writes and instance transitions require
// owner/admin/member. Provider references are opaque handles only: APIs
// never return kubeconfigs, tokens, or cloud credentials.
type EnvironmentService interface {
	Components() []environment.Component
	Profiles() []environment.Profile
	Providers() []environment.ProviderInfo
	ProviderHealth(context.Context, string) (environment.ProviderHealth, error)
	SetRequirements(context.Context, auth.Principal, string, string, string, environment.Requirements) (environment.Requirements, error)
	GetRequirements(context.Context, auth.Principal, string, string, string) (environment.Requirements, bool, error)
	Resolve(context.Context, auth.Principal, string, environment.ResolveRequest) (environment.SchedulerDecision, environment.Requirements, error)
	GetInstance(context.Context, auth.Principal, string, string) (environment.EnvironmentInstance, error)
	ListInstances(context.Context, auth.Principal, string, int) ([]environment.EnvironmentInstance, error)
	TransitionInstance(context.Context, auth.Principal, string, string, string) (environment.EnvironmentInstance, error)
}

type environmentHandlers struct {
	service EnvironmentService
	logger  *slog.Logger
}

func (h environmentHandlers) listComponents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, listResponse[environment.Component]{Items: h.service.Components(), Pagination: paginationResponse{Limit: len(h.service.Components()), Offset: 0}})
}

func (h environmentHandlers) listProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := h.service.Profiles()
	writeJSON(w, http.StatusOK, listResponse[environment.Profile]{Items: profiles, Pagination: paginationResponse{Limit: len(profiles), Offset: 0}})
}

func (h environmentHandlers) listProviders(w http.ResponseWriter, r *http.Request) {
	providers := h.service.Providers()
	writeJSON(w, http.StatusOK, listResponse[environment.ProviderInfo]{Items: providers, Pagination: paginationResponse{Limit: len(providers), Offset: 0}})
}

func (h environmentHandlers) listInstances(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListInstances(r.Context(), principal, workspaceID, 50)
	if handleEnvironmentError(w, h.logger, err) {
		return
	}
	if items == nil {
		items = []environment.EnvironmentInstance{}
	}
	writeJSON(w, http.StatusOK, listResponse[environment.EnvironmentInstance]{Items: items, Pagination: paginationResponse{Limit: len(items), Offset: 0}})
}

func (h environmentHandlers) getInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	environmentID, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	instance, err := h.service.GetInstance(r.Context(), principal, workspaceID, environmentID)
	if handleEnvironmentError(w, h.logger, err) {
		return
	}
	writeJSON(w, http.StatusOK, instance.SafeView())
}

type resolveSelectionRequest struct {
	ScenarioID        string `json:"scenario_id"`
	ScenarioVersionID string `json:"scenario_version_id"`
	Purpose           string `json:"purpose,omitempty"`
	ProfilePreference string `json:"profile_preference,omitempty"`
}

// resolveSelection is a diagnostic/matching endpoint. It accepts a
// ScenarioVersion reference, never arbitrary caller requirements: placement
// derives from stored server-reviewed requirements (or the legacy default).
func (h environmentHandlers) resolveSelection(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request resolveSelectionRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	decision, requirements, err := h.service.Resolve(r.Context(), principal, workspaceID, environment.ResolveRequest{
		ScenarioID:        request.ScenarioID,
		ScenarioVersionID: request.ScenarioVersionID,
		Purpose:           request.Purpose,
		ProfilePreference: request.ProfilePreference,
	})
	if handleEnvironmentError(w, h.logger, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requirements": requirements,
		"decision":     decision,
	})
}

func (h environmentHandlers) getVersionEnvironment(w http.ResponseWriter, r *http.Request) {
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
	requirements, explicit, err := h.service.GetRequirements(r.Context(), principal, workspaceID, scenarioID, versionID)
	if handleEnvironmentError(w, h.logger, err) {
		return
	}
	decision, _, resolveErr := h.service.Resolve(r.Context(), principal, workspaceID, environment.ResolveRequest{
		ScenarioID: scenarioID, ScenarioVersionID: versionID, Purpose: "trial",
	})
	if resolveErr != nil {
		if handleEnvironmentError(w, h.logger, resolveErr) {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scenario_version_id": versionID,
		"explicit":            explicit,
		"requirements":        requirements,
		"eligible_profile":    decision.Profile,
		"eligible_provider":   decision.Provider,
		"scheduler_reason":    decision.ReasonCode,
	})
}

type putVersionRequirementsRequest struct {
	RequiredComponents     []string `json:"required_components"`
	MinimumIsolationTier   string   `json:"minimum_isolation_tier"`
	RequiredProfileCaps    []string `json:"required_profile_capabilities,omitempty"`
	RuntimeContractVersion string   `json:"runtime_contract_version,omitempty"`
}

func (h environmentHandlers) putVersionRequirements(w http.ResponseWriter, r *http.Request) {
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
	var request putVersionRequirementsRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	stored, err := h.service.SetRequirements(r.Context(), principal, workspaceID, scenarioID, versionID, environment.Requirements{
		RequiredComponents:   request.RequiredComponents,
		MinimumIsolation:     request.MinimumIsolationTier,
		RequiredCapabilities: request.RequiredProfileCaps,
		RuntimeContract:      request.RuntimeContractVersion,
	})
	if handleEnvironmentError(w, h.logger, err) {
		return
	}
	writeJSON(w, http.StatusOK, stored)
}

func handleEnvironmentError(w http.ResponseWriter, logger *slog.Logger, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		if logger != nil {
			logger.Error("environment request failed", "error", err)
		}
	}
	writeDomainError(w, err)
	return true
}
