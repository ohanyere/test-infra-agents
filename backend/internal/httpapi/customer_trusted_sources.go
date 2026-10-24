package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customercollector"
)

type customerTrustedSourceService interface {
	RegisterAuditMTLSSource(context.Context, auth.Principal, string, string, string) (customercollector.AuditMTLSSource, error)
	ListAuditMTLSSources(context.Context, auth.Principal, string, string) ([]customercollector.AuditMTLSSource, error)
	RevokeAuditMTLSSource(context.Context, auth.Principal, string, string, string) error
}
type customerTrustedSourceHandlers struct {
	service customerTrustedSourceService
	logger  *slog.Logger
}

func (h customerTrustedSourceHandlers) registerAudit(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	eid, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	var input struct {
		CertificatePEM string `json:"certificate_pem"`
	}
	if !decodeLimited(w, r, &input, 16<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.RegisterAuditMTLSSource(r.Context(), p, wid, eid, input.CertificatePEM)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h customerTrustedSourceHandlers) listAudit(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	eid, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListAuditMTLSSources(r.Context(), p, wid, eid)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[customercollector.AuditMTLSSource]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h customerTrustedSourceHandlers) revokeAudit(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	eid, ok := pathUUID(w, r, "environmentID")
	if !ok {
		return
	}
	sid, ok := pathUUID(w, r, "sourceID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if (customerCollectorHandlers{logger: h.logger}).handle(w, h.service.RevokeAuditMTLSSource(r.Context(), p, wid, eid, sid)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
