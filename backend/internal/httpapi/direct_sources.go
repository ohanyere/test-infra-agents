package httpapi

import (
	"context"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"net/http"
)

type directSourceService interface {
	RegisterDirectSource(context.Context, auth.Principal, string, string, operational.DirectSource) (operational.DirectSource, error)
	ListDirectSources(context.Context, auth.Principal, string, string) ([]operational.DirectSource, error)
	RevokeDirectSource(context.Context, auth.Principal, string, string, string) error
	ListDirectObservations(context.Context, auth.Principal, string, string, string) ([]operational.DirectObservation, error)
}
type directSourceHandlers struct {
	service             directSourceService
	operationalHandlers operationalHandlers
}

func (h directSourceHandlers) register(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.operationalHandlers.ids(w, r)
	if !ok {
		return
	}
	var in operational.DirectSource
	if !decodeLimited(w, r, &in, 16<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.RegisterDirectSource(r.Context(), p, wid, eid, in)
	if h.operationalHandlers.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h directSourceHandlers) list(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.operationalHandlers.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListDirectSources(r.Context(), p, wid, eid)
	if h.operationalHandlers.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.DirectSource]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h directSourceHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.operationalHandlers.ids(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "sourceID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if h.operationalHandlers.handle(w, h.service.RevokeDirectSource(r.Context(), p, wid, eid, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h directSourceHandlers) observations(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.operationalHandlers.ids(w, r)
	if !ok {
		return
	}
	sid, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListDirectObservations(r.Context(), p, wid, eid, sid)
	if h.operationalHandlers.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.DirectObservation]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
