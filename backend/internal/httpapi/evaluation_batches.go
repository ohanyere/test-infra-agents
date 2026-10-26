package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"log/slog"
	"net/http"
)

type EvaluationBatchService interface {
	Create(context.Context, auth.Principal, string, evaluationbatch.CreateInput) (evaluationbatch.Batch, error)
	List(context.Context, auth.Principal, string, evaluationbatch.Filters) ([]evaluationbatch.Batch, error)
	Get(context.Context, auth.Principal, string, string) (evaluationbatch.Batch, error)
	ListRuns(context.Context, auth.Principal, string, string, evaluationbatch.Filters) ([]evaluationbatch.Run, error)
	GetRun(context.Context, auth.Principal, string, string) (evaluationbatch.Run, error)
}
type evaluationBatchHandlers struct {
	service EvaluationBatchService
	logger  *slog.Logger
}
type createBatchRequest struct {
	ScenarioVersionID string                         `json:"scenario_version_id"`
	AgentVersionIDs   []string                       `json:"agent_version_ids"`
	RunsPerAgent      int                            `json:"runs_per_agent"`
	EnvironmentMode   string                         `json:"environment_mode"`
	Config            json.RawMessage                `json:"config"`
	SourceMetadata    evaluationbatch.SourceMetadata `json:"source_metadata"`
	IdempotencyKey    string                         `json:"idempotency_key"`
}

func (h evaluationBatchHandlers) create(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var q createBatchRequest
	if decodeJSON(w, r, &q) != nil {
		writeAPIError(w, 400, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.Create(r.Context(), p, wid, evaluationbatch.CreateInput{ScenarioVersionID: q.ScenarioVersionID, AgentVersionIDs: q.AgentVersionIDs, RunsPerAgent: q.RunsPerAgent, EnvironmentMode: q.EnvironmentMode, Config: q.Config, SourceMetadata: q.SourceMetadata, IdempotencyKey: q.IdempotencyKey})
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 201, v)
}
func (h evaluationBatchHandlers) list(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	f, ok := batchFilters(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.List(r.Context(), p, wid, f)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, listResponse[evaluationbatch.Batch]{Items: v, Pagination: paginationResponse{Limit: f.Pagination.Limit, Offset: f.Pagination.Offset}})
}
func (h evaluationBatchHandlers) get(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	bid, ok := pathUUID(w, r, "batchID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.Get(r.Context(), p, wid, bid)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, v)
}
func (h evaluationBatchHandlers) listRuns(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	bid, ok := pathUUID(w, r, "batchID")
	if !ok {
		return
	}
	f, ok := runFilters(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.ListRuns(r.Context(), p, wid, bid, f)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, listResponse[evaluationbatch.Run]{Items: v, Pagination: paginationResponse{Limit: f.Pagination.Limit, Offset: f.Pagination.Offset}})
}
func (h evaluationBatchHandlers) getRun(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "runID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.GetRun(r.Context(), p, wid, id)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, v)
}
func batchFilters(w http.ResponseWriter, r *http.Request) (evaluationbatch.Filters, bool) {
	p, ok := parsePagination(w, r)
	if !ok {
		return evaluationbatch.Filters{}, false
	}
	f, e := evaluationbatch.ValidateFilters(evaluationbatch.Filters{Pagination: p, Status: r.URL.Query().Get("status")})
	if e != nil {
		writeDomainError(w, e)
		return f, false
	}
	return f, true
}
func runFilters(w http.ResponseWriter, r *http.Request) (evaluationbatch.Filters, bool) {
	p, ok := parsePagination(w, r)
	if !ok {
		return evaluationbatch.Filters{}, false
	}
	f, e := evaluationbatch.ValidateRunFilters(evaluationbatch.Filters{Pagination: p, Status: r.URL.Query().Get("status")})
	if e != nil {
		writeDomainError(w, e)
		return f, false
	}
	return f, true
}
func (h evaluationBatchHandlers) handle(w http.ResponseWriter, e error) bool {
	if e == nil {
		return false
	}
	if errors.Is(e, auth.ErrForbidden) || errors.Is(e, auth.ErrUnauthenticated) {
		writeAuthError(w, e)
		return true
	}
	if !errors.Is(e, identity.ErrValidation) && !errors.Is(e, identity.ErrNotFound) && !errors.Is(e, identity.ErrConflict) {
		h.logger.Error("evaluation batch request failed", "error", e)
	}
	writeDomainError(w, e)
	return true
}
