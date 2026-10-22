package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
)

type OperationalMemoryService interface {
	MaterializeSession(context.Context, auth.Principal, string, string, string) ([]operational.Episode, error)
	ListEpisodes(context.Context, auth.Principal, string, string, bool) ([]operational.Episode, error)
	ListEpisodeRelationships(context.Context, auth.Principal, string, string, string) ([]operational.EpisodeRelationship, error)
	DecideEpisode(context.Context, auth.Principal, string, string, string, string, string) error
	ListLessons(context.Context, auth.Principal, string, string) ([]operational.Lesson, error)
	ReviewLesson(context.Context, auth.Principal, string, string, string, string, string) error
	SupersedeLesson(context.Context, auth.Principal, string, string, string, string) (string, error)
	ListLessonDecisions(context.Context, auth.Principal, string, string, string) ([]operational.LessonDecision, error)
	IssueGuidanceCredential(context.Context, auth.Principal, string, string, operational.GuidanceCredentialInput) (operational.GuidanceCredential, error)
	RevokeGuidanceCredential(context.Context, auth.Principal, string, string, string) error
	ListGuidanceCredentials(context.Context, auth.Principal, string, string) ([]operational.GuidanceCredential, error)
	GuidanceForAgent(context.Context, string, operational.ProposedOperation) (operational.Guidance, error)
	PreviewGuidance(context.Context, auth.Principal, string, string, operational.ProposedOperation) (operational.Guidance, error)
	ListGuidanceHistory(context.Context, auth.Principal, string, string) ([]operational.Guidance, error)
	ListGuidanceOperationLinks(context.Context, auth.Principal, string, string) ([]operational.GuidanceOperationLink, error)
	RecordGuidanceExperiment(context.Context, auth.Principal, string, operational.ExperimentArm) (operational.ExperimentRecord, error)
	ListGuidanceExperiments(context.Context, auth.Principal, string) ([]operational.ExperimentPair, error)
}
type memoryHandlers struct {
	service OperationalMemoryService
	logger  *slog.Logger
}

func (h memoryHandlers) ids(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	a, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	b, ok := pathUUID(w, r, "environmentID")
	return a, b, ok
}
func (h memoryHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
	} else {
		h.logger.Error("operational memory request failed", "error", err)
		writeDomainError(w, err)
	}
	return true
}
func (h memoryHandlers) episodes(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListEpisodes(r.Context(), p, a, b, r.URL.Query().Get("include_provisional") == "true")
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.Episode]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}

func (h memoryHandlers) relationships(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "episodeID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListEpisodeRelationships(r.Context(), p, a, b, id)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.EpisodeRelationship]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h memoryHandlers) materialize(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.MaterializeSession(r.Context(), p, a, b, id)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.Episode]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h memoryHandlers) lessons(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListLessons(r.Context(), p, a, b)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.Lesson]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}

type decisionInput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (h memoryHandlers) episodeDecision(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "episodeID")
	if !ok {
		return
	}
	var in decisionInput
	if !decodeLimited(w, r, &in, 4096) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if h.handle(w, h.service.DecideEpisode(r.Context(), p, a, b, id, in.Decision, in.Reason)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h memoryHandlers) lessonDecision(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "lessonID")
	if !ok {
		return
	}
	var in decisionInput
	if !decodeLimited(w, r, &in, 4096) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if h.handle(w, h.service.ReviewLesson(r.Context(), p, a, b, id, in.Decision, in.Reason)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h memoryHandlers) lessonDecisions(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "lessonID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListLessonDecisions(r.Context(), p, a, b, id)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.LessonDecision]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h memoryHandlers) supersedeLesson(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "lessonID")
	if !ok {
		return
	}
	var in struct {
		ApplicabilityNote string `json:"applicability_note"`
	}
	if !decodeLimited(w, r, &in, 2048) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.SupersedeLesson(r.Context(), p, a, b, id, in.ApplicabilityNote)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"lesson_id": out})
}
func (h memoryHandlers) issueCredential(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	var in operational.GuidanceCredentialInput
	if !decodeLimited(w, r, &in, 8192) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.IssueGuidanceCredential(r.Context(), p, a, b, in)
	if h.handle(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, out)
}
func (h memoryHandlers) revokeCredential(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "credentialID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if h.handle(w, h.service.RevokeGuidanceCredential(r.Context(), p, a, b, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h memoryHandlers) preview(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	var in operational.ProposedOperation
	if !decodeLimited(w, r, &in, 8192) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.PreviewGuidance(r.Context(), p, a, b, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h memoryHandlers) history(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListGuidanceHistory(r.Context(), p, a, b)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.Guidance]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h memoryHandlers) guidanceLinks(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListGuidanceOperationLinks(r.Context(), p, a, b)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.GuidanceOperationLink]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h memoryHandlers) agentGuidance(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeAuthError(w, auth.ErrUnauthenticated)
		return
	}
	var in operational.ProposedOperation
	if !decodeLimited(w, r, &in, 8192) {
		return
	}
	out, err := h.service.GuidanceForAgent(r.Context(), strings.TrimPrefix(header, "Bearer "), in)
	if h.handle(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (h memoryHandlers) recordExperiment(w http.ResponseWriter, r *http.Request) {
	wID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var in operational.ExperimentArm
	if !decodeLimited(w, r, &in, 256<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.RecordGuidanceExperiment(r.Context(), p, wID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h memoryHandlers) experiments(w http.ResponseWriter, r *http.Request) {
	wID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListGuidanceExperiments(r.Context(), p, wID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.ExperimentPair]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}

func (h memoryHandlers) credentials(w http.ResponseWriter, r *http.Request) {
	a, b, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListGuidanceCredentials(r.Context(), p, a, b)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.GuidanceCredential]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
