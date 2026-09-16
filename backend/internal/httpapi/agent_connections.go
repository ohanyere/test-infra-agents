package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/agentcapability"
	"github.com/evalplane/evalplane/backend/internal/agentconnection"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type AgentConnectionService interface {
	Get(context.Context, auth.Principal, string, string) (agentconnection.Info, error)
	UpdateCredential(context.Context, auth.Principal, string, string, agentconnection.CredentialInput) (agentconnection.Info, error)
	Test(context.Context, auth.Principal, string, string) (agentconnection.Info, error)
	Compatibility(context.Context, auth.Principal, string, string, string) (agentconnection.Compatibility, error)
	TestConfiguration(context.Context, auth.Principal, string, agentconnection.TestConfigurationInput) (agentconnection.TestResult, error)
}

type agentConnectionHandlers struct {
	service AgentConnectionService
	logger  *slog.Logger
}
type credentialRequest struct {
	AuthType    string `json:"auth_type"`
	BearerToken string `json:"bearer_token"`
}
type testConfigurationRequest struct {
	EndpointURL string `json:"endpoint_url"`
	AuthType    string `json:"auth_type"`
	BearerToken string `json:"bearer_token"`
}

func (h agentConnectionHandlers) catalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": agentcapability.List()})
}
func (h agentConnectionHandlers) get(w http.ResponseWriter, r *http.Request) {
	workspaceID, versionID, ok := connectionPath(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Get(r.Context(), p, workspaceID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentConnectionHandlers) credential(w http.ResponseWriter, r *http.Request) {
	workspaceID, versionID, ok := connectionPath(w, r)
	if !ok {
		return
	}
	var request credentialRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.UpdateCredential(r.Context(), p, workspaceID, versionID, agentconnection.CredentialInput{AuthType: request.AuthType, BearerToken: request.BearerToken})
	request.BearerToken = ""
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentConnectionHandlers) test(w http.ResponseWriter, r *http.Request) {
	workspaceID, versionID, ok := connectionPath(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Test(r.Context(), p, workspaceID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentConnectionHandlers) testConfiguration(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request testConfigurationRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.TestConfiguration(r.Context(), p, workspaceID, agentconnection.TestConfigurationInput{EndpointURL: request.EndpointURL, AuthType: request.AuthType, BearerToken: request.BearerToken})
	request.BearerToken = ""
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h agentConnectionHandlers) compatibility(w http.ResponseWriter, r *http.Request) {
	workspaceID, versionID, ok := connectionPath(w, r)
	if !ok {
		return
	}
	scenarioVersionID, ok := pathUUID(w, r, "scenarioVersionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.Compatibility(r.Context(), p, workspaceID, versionID, scenarioVersionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func connectionPath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	versionID, ok := pathUUID(w, r, "versionID")
	return workspaceID, versionID, ok
}
func (h agentConnectionHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) {
		h.logger.Error("agent connection request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
