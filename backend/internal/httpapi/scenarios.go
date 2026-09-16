package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
)

type ScenarioService interface {
	ListPublic(context.Context, scenario.Filters) ([]scenario.Scenario, error)
	GetPublic(context.Context, string) (scenario.Scenario, error)
	ListWorkspace(context.Context, auth.Principal, string, scenario.Filters) ([]scenario.Scenario, error)
	GetWorkspace(context.Context, auth.Principal, string, string) (scenario.Scenario, error)
	Create(context.Context, auth.Principal, string, scenario.CreateInput) (scenario.Scenario, error)
	Update(context.Context, auth.Principal, string, string, scenario.UpdateInput) (scenario.Scenario, error)
	CreateVersion(context.Context, auth.Principal, string, string, scenario.VersionInput) (scenario.Version, error)
	ListVersions(context.Context, auth.Principal, string, string, identity.Pagination) ([]scenario.Version, error)
	GetVersion(context.Context, auth.Principal, string, string, string) (scenario.Version, error)
}
type scenarioHandlers struct {
	service ScenarioService
	logger  *slog.Logger
}
type criterionRequest struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	MetricKey   string          `json:"metric_key"`
	Operator    string          `json:"operator"`
	TargetValue json.RawMessage `json:"target_value"`
	Unit        *string         `json:"unit"`
	SortOrder   int             `json:"sort_order"`
}
type scenarioVersionRequest struct {
	Description         string             `json:"description"`
	Category            string             `json:"category"`
	Difficulty          string             `json:"difficulty"`
	DurationSeconds     int                `json:"duration_seconds"`
	Environment         json.RawMessage    `json:"environment"`
	Conditions          json.RawMessage    `json:"conditions"`
	Observables         []string           `json:"observables"`
	ExpectedBehaviors   []string           `json:"expected_behaviors"`
	ForbiddenBehaviors  []string           `json:"forbidden_behaviors"`
	Criteria            []criterionRequest `json:"criteria"`
	AuthoringMethod     string             `json:"authoring_method"`
	AgentTask           string             `json:"agent_task"`
	AgentVisibleContext json.RawMessage    `json:"agent_visible_context"`
	AllowedActions      []string           `json:"allowed_actions"`
	AllowedResources    json.RawMessage    `json:"allowed_resources"`
	GroundTruth         json.RawMessage    `json:"ground_truth"`
	ExecutionSpec       json.RawMessage    `json:"execution_spec"`
	SourceMetadata      json.RawMessage    `json:"source_metadata"`
}
type createScenarioRequest struct {
	Name           string                 `json:"name"`
	Slug           string                 `json:"slug"`
	Visibility     string                 `json:"visibility"`
	InitialVersion scenarioVersionRequest `json:"initial_version"`
}
type updateScenarioRequest struct {
	Name       *string `json:"name"`
	Visibility *string `json:"visibility"`
	Status     *string `json:"status"`
}

func (h scenarioHandlers) listPublic(w http.ResponseWriter, r *http.Request) {
	f, ok := parseScenarioFilters(w, r, true)
	if !ok {
		return
	}
	v, e := h.service.ListPublic(r.Context(), f)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, listResponse[scenario.Scenario]{Items: v, Pagination: paginationResponse{Limit: f.Pagination.Limit, Offset: f.Pagination.Offset}})
}
func (h scenarioHandlers) getPublic(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.GetPublic(r.Context(), chi.URLParam(r, "slug"))
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, v)
}
func (h scenarioHandlers) listWorkspace(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	f, ok := parseScenarioFilters(w, r, false)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.ListWorkspace(r.Context(), p, wid, f)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, listResponse[scenario.Scenario]{Items: v, Pagination: paginationResponse{Limit: f.Pagination.Limit, Offset: f.Pagination.Offset}})
}
func (h scenarioHandlers) create(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var q createScenarioRequest
	if !decodeScenario(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.Create(r.Context(), p, wid, scenario.CreateInput{Name: q.Name, Slug: q.Slug, Visibility: q.Visibility, InitialVersion: scenarioVersionInput(q.InitialVersion)})
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 201, v)
}
func (h scenarioHandlers) getWorkspace(w http.ResponseWriter, r *http.Request) {
	wid, sid, ok := scenarioIDs(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.GetWorkspace(r.Context(), p, wid, sid)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, v)
}
func (h scenarioHandlers) update(w http.ResponseWriter, r *http.Request) {
	wid, sid, ok := scenarioIDs(w, r)
	if !ok {
		return
	}
	var q updateScenarioRequest
	if !decodeScenario(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.Update(r.Context(), p, wid, sid, scenario.UpdateInput{Name: q.Name, Visibility: q.Visibility, Status: q.Status})
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, v)
}
func (h scenarioHandlers) createVersion(w http.ResponseWriter, r *http.Request) {
	wid, sid, ok := scenarioIDs(w, r)
	if !ok {
		return
	}
	var q scenarioVersionRequest
	if !decodeScenario(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.CreateVersion(r.Context(), p, wid, sid, scenarioVersionInput(q))
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 201, v)
}
func (h scenarioHandlers) listVersions(w http.ResponseWriter, r *http.Request) {
	wid, sid, ok := scenarioIDs(w, r)
	if !ok {
		return
	}
	page, ok := parsePagination(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.ListVersions(r.Context(), p, wid, sid, page)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, listResponse[scenario.Version]{Items: v, Pagination: paginationResponse{Limit: page.Limit, Offset: page.Offset}})
}
func (h scenarioHandlers) getVersion(w http.ResponseWriter, r *http.Request) {
	wid, sid, ok := scenarioIDs(w, r)
	if !ok {
		return
	}
	vid, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.GetVersion(r.Context(), p, wid, sid, vid)
	if h.handle(w, e) {
		return
	}
	writeJSON(w, 200, v)
}
func scenarioIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	sid, ok := pathUUID(w, r, "scenarioID")
	return wid, sid, ok
}
func parseScenarioFilters(w http.ResponseWriter, r *http.Request, pub bool) (scenario.Filters, bool) {
	p, ok := parsePagination(w, r)
	if !ok {
		return scenario.Filters{}, false
	}
	f, e := scenario.ValidateFilters(scenario.Filters{Pagination: p, Query: r.URL.Query().Get("q"), Category: r.URL.Query().Get("category"), Difficulty: r.URL.Query().Get("difficulty"), Status: r.URL.Query().Get("status")}, pub)
	if e != nil {
		writeDomainError(w, e)
		return f, false
	}
	return f, true
}
func scenarioVersionInput(q scenarioVersionRequest) scenario.VersionInput {
	criteria := make([]scenario.CriterionInput, len(q.Criteria))
	for i, c := range q.Criteria {
		criteria[i] = scenario.CriterionInput{Key: c.Key, Name: c.Name, MetricKey: c.MetricKey, Operator: c.Operator, TargetValue: c.TargetValue, Unit: c.Unit, SortOrder: c.SortOrder}
	}
	return scenario.VersionInput{Description: q.Description, Category: q.Category, Difficulty: q.Difficulty, DurationSeconds: q.DurationSeconds, Environment: q.Environment, Conditions: q.Conditions, Observables: q.Observables, ExpectedBehaviors: q.ExpectedBehaviors, ForbiddenBehaviors: q.ForbiddenBehaviors, Criteria: criteria, AuthoringMethod: q.AuthoringMethod, AgentTask: q.AgentTask, AgentVisibleContext: q.AgentVisibleContext, AllowedActions: q.AllowedActions, AllowedResources: q.AllowedResources, GroundTruth: q.GroundTruth, ExecutionSpec: q.ExecutionSpec, SourceMetadata: q.SourceMetadata}
}
func decodeScenario(w http.ResponseWriter, r *http.Request, v any) bool {
	if decodeJSON(w, r, v) != nil {
		writeAPIError(w, 400, "validation_error", "request body must contain one valid JSON document with known fields")
		return false
	}
	return true
}
func (h scenarioHandlers) handle(w http.ResponseWriter, e error) bool {
	if e == nil {
		return false
	}
	if errors.Is(e, auth.ErrForbidden) || errors.Is(e, auth.ErrUnauthenticated) {
		writeAuthError(w, e)
		return true
	}
	if !errors.Is(e, identity.ErrValidation) && !errors.Is(e, identity.ErrNotFound) && !errors.Is(e, identity.ErrConflict) {
		h.logger.Error("scenario request failed", "error", e)
	}
	writeDomainError(w, e)
	return true
}
