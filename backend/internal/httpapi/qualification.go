package httpapi

// NOTE: this file was reconstructed during the Kubernetes/SRE qualification
// catalog stage. The previous stage-29 work left router.go referencing
// qualification policy handlers without a corresponding handler file, so the
// httpapi package did not compile. The surface below mirrors the
// qualification.Service methods and the routes/tests that reference them; no
// behavior beyond faithful HTTP translation was added.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/qualification"
)

type QualificationService interface {
	CreatePolicy(context.Context, auth.Principal, string, qualification.CreatePolicyInput) (qualification.Policy, error)
	CreatePolicyVersion(context.Context, auth.Principal, string, string, qualification.CreateVersionInput) (qualification.PolicyVersion, error)
	GetPolicy(context.Context, auth.Principal, string, string) (qualification.Policy, error)
	GetPolicyVersion(context.Context, auth.Principal, string, string) (qualification.PolicyVersion, error)
	ListPolicies(context.Context, auth.Principal, string) ([]qualification.Policy, error)
	CreateAssessment(context.Context, auth.Principal, string, qualification.CreateAssessmentInput) (qualification.Assessment, error)
	GetAssessment(context.Context, auth.Principal, string, string) (qualification.Assessment, error)
	ComparativeReport(context.Context, auth.Principal, string, string, string) (qualification.ComparativeReport, error)
}

type qualificationHandlers struct {
	service QualificationService
	logger  *slog.Logger
}

func (h qualificationHandlers) createPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var in qualification.CreatePolicyInput
	if decodeJSON(w, r, &in) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.CreatePolicy(r.Context(), principal, workspaceID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h qualificationHandlers) listPolicies(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ListPolicies(r.Context(), principal, workspaceID)
	if h.handle(w, err) {
		return
	}
	if out == nil {
		out = []qualification.Policy{}
	}
	writeJSON(w, http.StatusOK, listResponse[qualification.Policy]{Items: out, Pagination: paginationResponse{Limit: len(out), Offset: 0}})
}

func (h qualificationHandlers) getPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	policyID, ok := pathUUID(w, r, "policyID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.GetPolicy(r.Context(), principal, workspaceID, policyID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h qualificationHandlers) createVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	policyID, ok := pathUUID(w, r, "policyID")
	if !ok {
		return
	}
	var in qualification.CreateVersionInput
	if decodeJSON(w, r, &in) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.CreatePolicyVersion(r.Context(), principal, workspaceID, policyID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h qualificationHandlers) getVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.GetPolicyVersion(r.Context(), principal, workspaceID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h qualificationHandlers) createAssessment(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var in qualification.CreateAssessmentInput
	if decodeJSON(w, r, &in) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.CreateAssessment(r.Context(), principal, workspaceID, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h qualificationHandlers) getAssessment(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	assessmentID, ok := pathUUID(w, r, "assessmentID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.GetAssessment(r.Context(), principal, workspaceID, assessmentID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h qualificationHandlers) comparison(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	executionID, ok := pathUUID(w, r, "executionID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ComparativeReport(r.Context(), principal, workspaceID, executionID, r.URL.Query().Get("policy_version_id"))
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h qualificationHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("qualification request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}

type FailureTaxonomyService interface {
	ForExecution(context.Context, auth.Principal, string, string) (failuretaxonomy.Results, error)
}

type failureTaxonomyHandlers struct {
	service FailureTaxonomyService
	logger  *slog.Logger
}

func (h failureTaxonomyHandlers) get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	executionID, ok := pathUUID(w, r, "executionID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ForExecution(r.Context(), principal, workspaceID, executionID)
	if err != nil {
		if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
			writeAuthError(w, err)
			return
		}
		if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
			h.logger.Error("failure taxonomy request failed", "error", err)
		}
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
