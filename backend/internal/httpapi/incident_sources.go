package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/incidentsource"
	"github.com/go-chi/chi/v5"
)

type IncidentSourceService interface {
	Create(context.Context, auth.Principal, string, incidentsource.CreateInput) (incidentsource.Connection, error)
	List(context.Context, auth.Principal, string) ([]incidentsource.Connection, error)
	Get(context.Context, auth.Principal, string, string) (incidentsource.Connection, error)
	RotateCredentials(context.Context, auth.Principal, string, string, incidentsource.CredentialInput) (incidentsource.Connection, error)
	Test(context.Context, auth.Principal, string, string) (incidentsource.Connection, error)
	ListIncidents(context.Context, auth.Principal, string, string, incidentsource.ListFilter) (incidentsource.IncidentPage, error)
	GetIncident(context.Context, auth.Principal, string, string, string) (incidentsource.NormalizedIncident, error)
	ImportIncident(context.Context, auth.Principal, string, string, string) (incidentsource.ImportResult, error)
}

type incidentSourceHandlers struct {
	service IncidentSourceService
	logger  *slog.Logger
}

type incidentSourceCreateRequest struct {
	Provider       string `json:"provider"`
	DisplayName    string `json:"display_name"`
	Site           string `json:"site"`
	APIToken       string `json:"api_token"`
	APIKey         string `json:"api_key"`
	ApplicationKey string `json:"application_key"`
}

type incidentSourceRotateRequest struct {
	APIToken       string `json:"api_token"`
	APIKey         string `json:"api_key"`
	ApplicationKey string `json:"application_key"`
}

func (h incidentSourceHandlers) create(w http.ResponseWriter, r *http.Request) {
	workspace, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request incidentSourceCreateRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	input := incidentsource.CreateInput{
		Provider:       request.Provider,
		DisplayName:    request.DisplayName,
		Site:           request.Site,
		APIToken:       request.APIToken,
		APIKey:         request.APIKey,
		ApplicationKey: request.ApplicationKey,
	}
	request.APIToken = ""
	request.APIKey = ""
	request.ApplicationKey = ""
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Create(r.Context(), principal, workspace, input)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h incidentSourceHandlers) list(w http.ResponseWriter, r *http.Request) {
	workspace, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.List(r.Context(), principal, workspace)
	if h.handle(w, err) {
		return
	}
	if items == nil {
		items = []incidentsource.Connection{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h incidentSourceHandlers) get(w http.ResponseWriter, r *http.Request) {
	workspace, connectionID, ok := incidentSourcePath(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Get(r.Context(), principal, workspace, connectionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h incidentSourceHandlers) rotate(w http.ResponseWriter, r *http.Request) {
	workspace, connectionID, ok := incidentSourcePath(w, r)
	if !ok {
		return
	}
	var request incidentSourceRotateRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	input := incidentsource.CredentialInput{APIToken: request.APIToken, APIKey: request.APIKey, ApplicationKey: request.ApplicationKey}
	request.APIToken = ""
	request.APIKey = ""
	request.ApplicationKey = ""
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.RotateCredentials(r.Context(), principal, workspace, connectionID, input)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h incidentSourceHandlers) test(w http.ResponseWriter, r *http.Request) {
	workspace, connectionID, ok := incidentSourcePath(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Test(r.Context(), principal, workspace, connectionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h incidentSourceHandlers) browse(w http.ResponseWriter, r *http.Request) {
	workspace, connectionID, ok := incidentSourcePath(w, r)
	if !ok {
		return
	}
	filter, ok := incidentBrowseFilter(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	page, err := h.service.ListIncidents(r.Context(), principal, workspace, connectionID, filter)
	if h.handle(w, err) {
		return
	}
	if page.Items == nil {
		page.Items = []incidentsource.NormalizedIncident{}
	}
	writeJSON(w, http.StatusOK, page)
}

func (h incidentSourceHandlers) fetch(w http.ResponseWriter, r *http.Request) {
	workspace, connectionID, ok := incidentSourcePath(w, r)
	if !ok {
		return
	}
	externalID := chi.URLParam(r, "externalID")
	if externalID == "" || len(externalID) > 200 {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "external incident id is invalid")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	incident, err := h.service.GetIncident(r.Context(), principal, workspace, connectionID, externalID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (h incidentSourceHandlers) importIncident(w http.ResponseWriter, r *http.Request) {
	workspace, connectionID, ok := incidentSourcePath(w, r)
	if !ok {
		return
	}
	externalID := chi.URLParam(r, "externalID")
	if externalID == "" || len(externalID) > 200 {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "external incident id is invalid")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	result, err := h.service.ImportIncident(r.Context(), principal, workspace, connectionID, externalID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func incidentSourcePath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspace, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	connectionID, ok := pathUUID(w, r, "connectionID")
	if !ok {
		return "", "", false
	}
	return workspace, connectionID, true
}

func incidentBrowseFilter(w http.ResponseWriter, r *http.Request) (incidentsource.ListFilter, bool) {
	filter := incidentsource.ListFilter{Limit: incidentsource.DefaultPageSize}
	query := r.URL.Query()
	filter.Query = query.Get("query")
	if len(filter.Query) > 200 {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "query must be at most 200 characters")
		return filter, false
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > incidentsource.MaxPageSize {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "limit must be an integer between 1 and 50")
			return filter, false
		}
		filter.Limit = limit
	}
	if raw := query.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 || offset > 100000 {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "offset must be a non-negative integer")
			return filter, false
		}
		filter.Offset = offset
	}
	return filter, true
}

func (h incidentSourceHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if errors.Is(err, identity.ErrValidation) {
		writeDomainError(w, err)
		return true
	}
	if errors.Is(err, identity.ErrNotFound) {
		writeDomainError(w, err)
		return true
	}
	var providerError *incidentsource.ProviderError
	if errors.As(err, &providerError) {
		writeProviderError(w, providerError)
		return true
	}
	h.logger.Error("incident source request failed", "error", err)
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	return true
}

func writeProviderError(w http.ResponseWriter, providerError *incidentsource.ProviderError) {
	switch providerError.Code {
	case incidentsource.CodePagerDutyAuthenticationFailed, incidentsource.CodeDatadogAuthenticationFailed:
		writeAPIError(w, http.StatusUnauthorized, "authentication_failed", "provider authentication failed")
	case incidentsource.CodePagerDutyPermissionRequired, incidentsource.CodeDatadogPermissionRequired:
		writeAPIError(w, http.StatusForbidden, "permission_required", "provider permission required")
	case incidentsource.CodeDatadogInvalidSite:
		writeAPIError(w, http.StatusBadRequest, "invalid_configuration", "unsupported Datadog site")
	case incidentsource.CodeProviderTimeout:
		writeAPIError(w, http.StatusGatewayTimeout, "timeout", "provider request timed out")
	case incidentsource.CodeProviderNotFound:
		writeAPIError(w, http.StatusNotFound, "not_found", "provider incident not found")
	case incidentsource.CodeProviderRateLimited:
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "provider rate limit exceeded")
	case incidentsource.CodeProviderUnreachable:
		writeAPIError(w, http.StatusBadGateway, "provider_unavailable", "provider is unreachable")
	default:
		writeAPIError(w, http.StatusBadGateway, "provider_error", "provider request failed")
	}
}
