package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
)

// CertificationService is implemented by scenariocertification.Service. Reads
// require any workspace membership (viewer may read); certification triggers
// and reference authoring require owner/admin/member. Certification state is
// server-derived: no endpoint accepts state=certified.
type CertificationService interface {
	Certify(context.Context, auth.Principal, string, string, string, scenariocertification.CertifyRequest) (scenariocertification.Certification, error)
	GetStatus(context.Context, auth.Principal, string, string, string) (scenariocertification.CertificationStatus, error)
	PutReference(context.Context, auth.Principal, string, string, string, scenariocertification.ReferenceRemediationInput) (scenariocertification.ReferenceRemediation, error)
	EligibleAuthoritativeEvidence(context.Context, auth.Principal, string, string, string) (bool, error)
}

type certificationHandlers struct {
	service CertificationService
	logger  *slog.Logger
}

// certifyHTTPRequest is the minimal public certification trigger. Only
// idempotency_key, environment_profile, and runtime_contract_version are
// accepted. Because decodeJSON uses DisallowUnknownFields, simulate,
// variant_configs, reference_remediation, state, certified, gate_results,
// failure_codes, and evaluator_version all return 400.
type certifyHTTPRequest struct {
	IdempotencyKey         *string `json:"idempotency_key,omitempty"`
	RuntimeContractVersion string  `json:"runtime_contract_version,omitempty"`
	EnvironmentProfile     string  `json:"environment_profile,omitempty"`
}

type referenceRemediationHTTPInput struct {
	Preconditions     json.RawMessage                         `json:"preconditions"`
	ReferenceActions  []scenariocertification.ReferenceAction `json:"reference_actions"`
	ExpectedState     json.RawMessage                         `json:"expected_resulting_state"`
	VerificationNotes string                                  `json:"verification_notes"`
	CleanupExpected   string                                  `json:"cleanup_expectations"`
}

func (h certificationHandlers) getCertification(w http.ResponseWriter, r *http.Request) {
	workspaceID, scenarioID, versionID, ok := certificationIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	status, err := h.service.GetStatus(r.Context(), principal, workspaceID, scenarioID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h certificationHandlers) triggerCertification(w http.ResponseWriter, r *http.Request) {
	workspaceID, scenarioID, versionID, ok := certificationIDs(w, r)
	if !ok {
		return
	}
	var request certifyHTTPRequest
	if r.ContentLength != 0 {
		if decodeJSON(w, r, &request) != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
			return
		}
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	cert, err := h.service.Certify(r.Context(), principal, workspaceID, scenarioID, versionID, certifyHTTPRequestToService(request))
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, cert)
}

func (h certificationHandlers) putReference(w http.ResponseWriter, r *http.Request) {
	workspaceID, scenarioID, versionID, ok := certificationIDs(w, r)
	if !ok {
		return
	}
	var request referenceRemediationHTTPInput
	if decodeJSON(w, r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	ref, err := h.service.PutReference(r.Context(), principal, workspaceID, scenarioID, versionID, scenariocertification.ReferenceRemediationInput{
		Preconditions: request.Preconditions, ReferenceActions: request.ReferenceActions,
		ExpectedState: request.ExpectedState, VerificationNotes: request.VerificationNotes, CleanupExpected: request.CleanupExpected,
	})
	if h.handle(w, err) {
		return
	}
	// Never return reference contents: only safe metadata proving association.
	writeJSON(w, http.StatusCreated, map[string]any{
		"scenario_version_id": ref.ScenarioVersionID,
		"contract_version":    ref.ContractVersion,
		"content_hash":        ref.ContentHash,
		"updated_at":          ref.UpdatedAt,
	})
}

func (h certificationHandlers) getEligibility(w http.ResponseWriter, r *http.Request) {
	workspaceID, scenarioID, versionID, ok := certificationIDs(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	eligible, err := h.service.EligibleAuthoritativeEvidence(r.Context(), principal, workspaceID, scenarioID, versionID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scenario_version_id":             versionID,
		"eligible_authoritative_evidence": eligible,
		"certification_contract_version":  scenariocertification.CertificationContractVersion,
	})
}

func certificationIDs(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return "", "", "", false
	}
	scenarioID, ok := pathUUID(w, r, "scenarioID")
	if !ok {
		return "", "", "", false
	}
	versionID, ok := pathUUID(w, r, "versionID")
	if !ok {
		return "", "", "", false
	}
	return workspaceID, scenarioID, versionID, true
}

func certifyHTTPRequestToService(request certifyHTTPRequest) scenariocertification.CertifyRequest {
	return scenariocertification.CertifyRequest{
		IdempotencyKey:         request.IdempotencyKey,
		RuntimeContractVersion: strings.TrimSpace(request.RuntimeContractVersion),
		EnvironmentProfile:     strings.TrimSpace(request.EnvironmentProfile),
	}
}

func (h certificationHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("scenario certification request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
