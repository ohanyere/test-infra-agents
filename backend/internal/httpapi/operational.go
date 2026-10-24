package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
)

type OperationalService interface {
	SaveContract(context.Context, auth.Principal, string, string, operational.ContractInput) (operational.Contract, error)
	ListContracts(context.Context, auth.Principal, string, string) ([]operational.Contract, error)
	AssessRun(context.Context, auth.Principal, string, string, string) (operational.Report, error)
	ListReports(context.Context, auth.Principal, string, string, string) ([]operational.Report, error)
	ResolveEvidence(context.Context, auth.Principal, string, string, string, string) (operational.EvidenceDetail, error)
	RecordApproval(context.Context, auth.Principal, string, string, string, operational.ApprovalInput) (operational.ApprovalRecord, error)
	ListApprovals(context.Context, auth.Principal, string, string, string) ([]operational.ApprovalRecord, error)
	ListFindings(context.Context, auth.Principal, string, string, string) ([]operational.FindingRecord, error)
	ListStabilityFindings(context.Context, auth.Principal, string, string, string) ([]operational.FindingRecord, error)
	ListEvidenceReferences(context.Context, auth.Principal, string, string, string) ([]operational.EvidenceRef, error)
	ListNotifications(context.Context, auth.Principal, string, string) ([]operational.NotificationEvent, error)
	ListSessions(context.Context, auth.Principal, string, string) ([]operational.SessionSummary, error)
	EnvironmentStatus(context.Context, auth.Principal, string, string) (operational.EnvironmentStatus, error)
}
type operationalHandlers struct {
	service OperationalService
	logger  *slog.Logger
}

func (h operationalHandlers) ids(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	wID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", false
	}
	eID, ok := pathUUID(w, r, "environmentID")
	return wID, eID, ok
}
func (h operationalHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
	} else {
		h.logger.Error("operational request failed", "error", err)
		writeDomainError(w, err)
	}
	return true
}
func (h operationalHandlers) saveContract(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	var in operational.ContractInput
	if !decodeLimited(w, r, &in, 64<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.SaveContract(r.Context(), p, wID, eID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h operationalHandlers) listContracts(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListContracts(r.Context(), p, wID, eID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.Contract]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) assess(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.AssessRun(r.Context(), p, wID, eID, sID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h operationalHandlers) reports(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListReports(r.Context(), p, wID, eID, sID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.Report]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) evidenceDetail(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ResolveEvidence(r.Context(), p, wID, eID, sID, r.URL.Query().Get("reference"))
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h operationalHandlers) approval(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	var in operational.ApprovalInput
	if !decodeLimited(w, r, &in, 8<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.RecordApproval(r.Context(), p, wID, eID, sID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h operationalHandlers) approvals(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListApprovals(r.Context(), p, wID, eID, sID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.ApprovalRecord]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) findings(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListFindings(r.Context(), p, wID, eID, sID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.FindingRecord]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) stability(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListStabilityFindings(r.Context(), p, wID, eID, sID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.FindingRecord]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) evidenceReferences(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	sID, ok := pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListEvidenceReferences(r.Context(), p, wID, eID, sID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.EvidenceRef]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) notifications(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListNotifications(r.Context(), p, wID, eID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.NotificationEvent]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) sessions(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListSessions(r.Context(), p, wID, eID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[operational.SessionSummary]{Items: out, Pagination: paginationResponse{Limit: len(out)}})
}
func (h operationalHandlers) status(w http.ResponseWriter, r *http.Request) {
	wID, eID, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.EnvironmentStatus(r.Context(), p, wID, eID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
