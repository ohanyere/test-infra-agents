package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/go-chi/chi/v5"
)

type AgentService interface {
	ListPublic(context.Context, agent.Filters) ([]agent.Agent, error)
	GetPublic(context.Context, string) (agent.Agent, error)
	ListWorkspace(context.Context, auth.Principal, string, agent.Filters) ([]agent.Agent, error)
	GetWorkspace(context.Context, auth.Principal, string, string) (agent.Agent, error)
	Create(context.Context, auth.Principal, string, agent.CreateInput) (agent.Agent, error)
	Update(context.Context, auth.Principal, string, string, agent.UpdateInput) (agent.Agent, error)
	CreateVersion(context.Context, auth.Principal, string, string, agent.VersionInput) (agent.Version, error)
	ListVersions(context.Context, auth.Principal, string, string, identity.Pagination) ([]agent.Version, error)
	GetVersion(context.Context, auth.Principal, string, string, string) (agent.Version, error)
	ListVersionClaims(context.Context, auth.Principal, string, string, string) ([]agent.CapabilityClaim, error)
}

type agentHandlers struct {
	service AgentService
	logger  *slog.Logger
}
type versionRequest struct {
	VersionLabel         string          `json:"version_label"`
	InterfaceKind        string          `json:"interface_kind"`
	ToolAccessMode       string          `json:"tool_access_mode"`
	EndpointURL          string          `json:"endpoint_url"`
	Capabilities         []string        `json:"capabilities"`
	ClaimedCapabilityIDs []string        `json:"claimed_capability_ids"`
	Metadata             json.RawMessage `json:"metadata"`
}
type createAgentRequest struct {
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    string         `json:"description"`
	Visibility     string         `json:"visibility"`
	InitialVersion versionRequest `json:"initial_version"`
}
type updateAgentRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Visibility  *string `json:"visibility"`
	Status      *string `json:"status"`
}

func (h agentHandlers) listPublic(w http.ResponseWriter, r *http.Request) {
	filters, ok := parseAgentFilters(w, r, true)
	if !ok {
		return
	}
	items, err := h.service.ListPublic(r.Context(), filters)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[agent.Agent]{Items: items, Pagination: paginationResponse{Limit: filters.Pagination.Limit, Offset: filters.Pagination.Offset}})
}
func (h agentHandlers) getPublic(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetPublic(r.Context(), chi.URLParam(r, "slug"))
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentHandlers) listWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	filters, ok := parseAgentFilters(w, r, false)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListWorkspace(r.Context(), p, workspaceID, filters)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[agent.Agent]{Items: items, Pagination: paginationResponse{Limit: filters.Pagination.Limit, Offset: filters.Pagination.Offset}})
}
func (h agentHandlers) create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request createAgentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Create(r.Context(), p, workspaceID, agent.CreateInput{Name: request.Name, Slug: request.Slug, Description: request.Description, Visibility: request.Visibility, InitialVersion: versionInput(request.InitialVersion)})
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h agentHandlers) getWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID, agentID, ok := agentPathIDs(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetWorkspace(r.Context(), p, workspaceID, agentID)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentHandlers) update(w http.ResponseWriter, r *http.Request) {
	workspaceID, agentID, ok := agentPathIDs(w, r)
	if !ok {
		return
	}
	var request updateAgentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Update(r.Context(), p, workspaceID, agentID, agent.UpdateInput{Name: request.Name, Description: request.Description, Visibility: request.Visibility, Status: request.Status})
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentHandlers) createVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, agentID, ok := agentPathIDs(w, r)
	if !ok {
		return
	}
	var request versionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.CreateVersion(r.Context(), p, workspaceID, agentID, versionInput(request))
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h agentHandlers) listVersions(w http.ResponseWriter, r *http.Request) {
	workspaceID, agentID, ok := agentPathIDs(w, r)
	if !ok {
		return
	}
	page, ok := parsePagination(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListVersions(r.Context(), p, workspaceID, agentID, page)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[agent.Version]{Items: items, Pagination: paginationResponse{Limit: page.Limit, Offset: page.Offset}})
}
func (h agentHandlers) getVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, agentID, ok := agentPathIDs(w, r)
	if !ok {
		return
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetVersion(r.Context(), p, workspaceID, agentID, versionID)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func parseAgentFilters(w http.ResponseWriter, r *http.Request, public bool) (agent.Filters, bool) {
	page, ok := parsePagination(w, r)
	if !ok {
		return agent.Filters{}, false
	}
	filters, err := agent.ValidateFilters(agent.Filters{Pagination: page, Query: r.URL.Query().Get("q"), Capability: r.URL.Query().Get("capability"), InterfaceKind: r.URL.Query().Get("interface_kind"), Status: r.URL.Query().Get("status")}, public)
	if err != nil {
		writeDomainError(w, err)
		return filters, false
	}
	return filters, true
}
func agentPathIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	agentID, ok := pathUUID(w, r, "agentID")
	return workspaceID, agentID, ok
}
func versionInput(request versionRequest) agent.VersionInput {
	return agent.VersionInput{VersionLabel: request.VersionLabel, InterfaceKind: request.InterfaceKind, ToolAccessMode: request.ToolAccessMode, EndpointURL: request.EndpointURL, Capabilities: request.Capabilities, ClaimedCapabilityIDs: request.ClaimedCapabilityIDs, Metadata: request.Metadata}
}
func (h agentHandlers) handleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("agent request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
