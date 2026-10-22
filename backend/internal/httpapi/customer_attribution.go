package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
)

type customerAttributionService interface {
	RegisterIssuer(context.Context, auth.Principal, string, string, string, string, string) (customerattribution.Issuer, error)
	ListIssuers(context.Context, auth.Principal, string, string) ([]customerattribution.Issuer, error)
	RevokeIssuer(context.Context, auth.Principal, string, string, string) error
	IngestReceipt(context.Context, auth.Principal, string, string, customerattribution.SignedReceipt) (customerattribution.StoredDecision, error)
	IngestGatewayReceipt(context.Context, string, customerattribution.SignedReceipt) (customerattribution.StoredDecision, error)
	ListDecisions(context.Context, auth.Principal, string, string, string) ([]customerattribution.StoredDecision, error)
}
type customerAttributionHandlers struct {
	service customerAttributionService
	logger  *slog.Logger
}

func (h customerAttributionHandlers) scope(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	wid, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	eid, ok := pathUUID(w, r, "environmentID")
	return wid, eid, ok
}
func (h customerAttributionHandlers) registerIssuer(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.scope(w, r)
	if !ok {
		return
	}
	var in struct {
		PublicKey              string `json:"public_key"`
		AgentVersionID         string `json:"agent_version_id"`
		AgentCertificateSHA256 string `json:"agent_certificate_sha256"`
	}
	if !decodeLimited(w, r, &in, 2048) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.RegisterIssuer(r.Context(), p, wid, eid, in.AgentVersionID, in.AgentCertificateSHA256, in.PublicKey)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h customerAttributionHandlers) listIssuers(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.scope(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListIssuers(r.Context(), p, wid, eid)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[customerattribution.Issuer]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h customerAttributionHandlers) revokeIssuer(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.scope(w, r)
	if !ok {
		return
	}
	issuer, ok := pathUUID(w, r, "issuerID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if (customerCollectorHandlers{logger: h.logger}).handle(w, h.service.RevokeIssuer(r.Context(), p, wid, eid, issuer)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h customerAttributionHandlers) ingestReceipt(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.scope(w, r)
	if !ok {
		return
	}
	var in customerattribution.SignedReceipt
	if !decodeLimited(w, r, &in, 16<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.IngestReceipt(r.Context(), p, wid, eid, in)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h customerAttributionHandlers) ingestGatewayReceipt(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || strings.Count(header, "Bearer ") != 1 {
		http.Error(w, "issuer credential required", http.StatusUnauthorized)
		return
	}
	var in customerattribution.SignedReceipt
	if !decodeLimited(w, r, &in, 16<<10) {
		return
	}
	out, err := h.service.IngestGatewayReceipt(r.Context(), strings.TrimPrefix(header, "Bearer "), in)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h customerAttributionHandlers) listDecisions(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.scope(w, r)
	if !ok {
		return
	}
	sid, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListDecisions(r.Context(), p, wid, eid, sid)
	if (customerCollectorHandlers{logger: h.logger}).handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[customerattribution.StoredDecision]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
