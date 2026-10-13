package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type CustomerCollectorService interface {
	Register(ctx context.Context, p auth.Principal, workspaceID string, input customercollector.CreateEnvironmentInput) (customercollector.RegisteredEnvironment, error)
	List(ctx context.Context, p auth.Principal, workspaceID string) ([]customercollector.Environment, error)
	Rotate(ctx context.Context, p auth.Principal, workspaceID, environmentID string) (customercollector.RegisteredEnvironment, error)
	Revoke(ctx context.Context, p auth.Principal, workspaceID, environmentID string) error
	StartSession(ctx context.Context, p auth.Principal, workspaceID, environmentID string, input customercollector.CreateSessionInput) (customercollector.Session, error)
	Ingest(ctx context.Context, sessionID, token string, input customercollector.EvidenceBatch) (customercollector.IngestResult, error)
}

type customerCollectorHandlers struct {
	service CustomerCollectorService
	logger  *slog.Logger
}

func (h customerCollectorHandlers) register(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var in customercollector.CreateEnvironmentInput
	if !decodeLimited(w, r, &in, 16<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.Register(r.Context(), p, wid, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h customerCollectorHandlers) list(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.List(r.Context(), p, wid)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[customercollector.Environment]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h customerCollectorHandlers) rotate(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	eid, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.Rotate(r.Context(), p, wid, eid)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h customerCollectorHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	eid, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if h.handle(w, h.service.Revoke(r.Context(), p, wid, eid)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h customerCollectorHandlers) startSession(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	eid, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	var in customercollector.CreateSessionInput
	if !decodeLimited(w, r, &in, 16<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.StartSession(r.Context(), p, wid, eid, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h customerCollectorHandlers) ingest(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	token, ok := customercollector.ParseBearer(r.Header.Get("Authorization"))
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "collector_unauthorized", "collector credential is invalid")
		return
	}
	var in customercollector.EvidenceBatch
	if !decodeLimited(w, r, &in, 512<<10) {
		return
	}
	out, err := h.service.Ingest(r.Context(), sessionID, token, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}
func (h customerCollectorHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "the requested customer environment or evidence is outside this identity's scope")
		return true
	}
	if errors.Is(err, auth.ErrUnauthenticated) {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return true
	}
	if errors.Is(err, identity.ErrValidation) || errors.Is(err, identity.ErrNotFound) || errors.Is(err, identity.ErrConflict) {
		writeDomainError(w, err)
		return true
	}
	h.logger.Error("customer collector request failed", "error", err)
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	return true
}

func decodeLimited(w http.ResponseWriter, r *http.Request, destination any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body is invalid or exceeds the size limit")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one JSON document")
		return false
	}
	return true
}
