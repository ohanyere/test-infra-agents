package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type EvaluationSuiteService interface {
	CreateSuite(context.Context, auth.Principal, string, evaluationsuite.CreateSuiteInput) (evaluationsuite.Suite, error)
	CreateSuiteVersion(context.Context, auth.Principal, string, string, evaluationsuite.CreateVersionInput) (evaluationsuite.Version, error)
	GetSuite(context.Context, auth.Principal, string, string) (evaluationsuite.Suite, error)
	ListSuites(context.Context, auth.Principal, string, evaluationsuite.Filters) ([]evaluationsuite.Suite, error)
	GetSuiteVersion(context.Context, auth.Principal, string, string) (evaluationsuite.Version, error)
	StartSuiteExecution(context.Context, auth.Principal, string, evaluationsuite.StartExecutionInput) (evaluationsuite.Execution, error)
	GetSuiteExecution(context.Context, auth.Principal, string, string) (evaluationsuite.Execution, error)
	GetSuiteResults(context.Context, auth.Principal, string, string) (evaluationsuite.Results, error)
}

type evaluationSuiteHandlers struct {
	service EvaluationSuiteService
	logger  *slog.Logger
}

type createSuiteRequest struct {
	Name string `json:"name"`
}

type createSuiteVersionRequest struct {
	Description string                          `json:"description"`
	Metadata    json.RawMessage                 `json:"metadata"`
	Scenarios   []evaluationsuite.ScenarioEntry `json:"scenarios"`
}

type startSuiteExecutionRequest struct {
	SuiteVersionID  string   `json:"suite_version_id"`
	AgentVersionIDs []string `json:"agent_version_ids"`
}

func (h evaluationSuiteHandlers) createSuite(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request createSuiteRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.CreateSuite(r.Context(), principal, workspaceID, evaluationsuite.CreateSuiteInput{Name: request.Name})
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h evaluationSuiteHandlers) listSuites(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	pagination, ok := parsePagination(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	values, err := h.service.ListSuites(r.Context(), principal, workspaceID, evaluationsuite.Filters{Pagination: pagination})
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[evaluationsuite.Suite]{Items: values, Pagination: paginationResponse{Limit: pagination.Limit, Offset: pagination.Offset}})
}

func (h evaluationSuiteHandlers) getSuite(w http.ResponseWriter, r *http.Request) {
	workspaceID, suiteID, ok := suitePathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetSuite(r.Context(), principal, workspaceID, suiteID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h evaluationSuiteHandlers) createVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, suiteID, ok := suitePathIDs(w, r)
	if !ok {
		return
	}
	var request createSuiteVersionRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.CreateSuiteVersion(r.Context(), principal, workspaceID, suiteID, evaluationsuite.CreateVersionInput{Description: request.Description, Metadata: request.Metadata, Scenarios: request.Scenarios})
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h evaluationSuiteHandlers) getVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetSuiteVersion(r.Context(), principal, workspaceID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h evaluationSuiteHandlers) startExecution(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request startSuiteExecutionRequest
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.StartSuiteExecution(r.Context(), principal, workspaceID, evaluationsuite.StartExecutionInput{SuiteVersionID: request.SuiteVersionID, AgentVersionIDs: request.AgentVersionIDs})
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h evaluationSuiteHandlers) getExecution(w http.ResponseWriter, r *http.Request) {
	workspaceID, executionID, ok := executionPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetSuiteExecution(r.Context(), principal, workspaceID, executionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h evaluationSuiteHandlers) getResults(w http.ResponseWriter, r *http.Request) {
	workspaceID, executionID, ok := executionPathIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetSuiteResults(r.Context(), principal, workspaceID, executionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func suitePathIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	suiteID, ok := pathUUID(w, r, "suiteID")
	return workspaceID, suiteID, ok
}

func executionPathIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	executionID, ok := pathUUID(w, r, "executionID")
	return workspaceID, executionID, ok
}

func (h evaluationSuiteHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("evaluation suite request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
