package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
)

type gatewayPreflightService interface {
	PreflightGateway(context.Context, string, operational.GatewayPreflightInput) (operational.GatewayPreflight, error)
	RevokeApproval(context.Context, auth.Principal, string, string, string, string, string) error
}
type gatewayPreflightHandlers struct {
	service             gatewayPreflightService
	operationalHandlers operationalHandlers
}

func (h gatewayPreflightHandlers) preflight(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		http.Error(w, "issuer credential required", http.StatusUnauthorized)
		return
	}
	var in operational.GatewayPreflightInput
	if !decodeLimited(w, r, &in, 4096) {
		return
	}
	out, err := h.service.PreflightGateway(r.Context(), strings.TrimPrefix(header, "Bearer "), in)
	if h.operationalHandlers.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h gatewayPreflightHandlers) revokeApproval(w http.ResponseWriter, r *http.Request) {
	wid, eid, ok := h.operationalHandlers.ids(w, r)
	if !ok {
		return
	}
	sid, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	approvalID, ok := pathUUID(w, r, "approvalID")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeLimited(w, r, &in, 2048) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if h.operationalHandlers.handle(w, h.service.RevokeApproval(r.Context(), p, wid, eid, sid, approvalID, in.Reason)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
