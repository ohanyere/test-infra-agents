package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/billing"
	"github.com/go-chi/chi/v5"
)

type BillingService interface {
	ProviderName() string
	GetAccount(context.Context, auth.Principal, string) (billing.BillingAccount, error)
	Entitlements(context.Context, auth.Principal, string, bool, string) ([]billing.EntitlementDecision, billing.BillingAccount, error)
	Usage(context.Context, auth.Principal, string) (billing.UsageSummary, billing.BillingAccount, error)
	CreateCheckoutSession(context.Context, auth.Principal, string, string) (string, error)
	CreatePortalSession(context.Context, auth.Principal, string) (string, error)
	HandleProviderWebhook(context.Context, string, []byte, string) (billing.ProviderEvent, error)
}

type billingHandlers struct {
	service BillingService
	logger  *slog.Logger
}

func (h billingHandlers) account(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.service.GetAccount(r.Context(), principal, workspaceID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": value, "catalog_contract": billing.BillingCatalogContractVersion, "plans": billing.Catalog(), "provider_backed": h.serviceProviderBacked()})
}

func (h billingHandlers) entitlements(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, account, err := h.service.Entitlements(r.Context(), principal, workspaceID, false, r.URL.Query().Get("required_isolation"))
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": account, "contract": billing.EntitlementContractVersion, "items": items})
}

func (h billingHandlers) usage(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	value, account, err := h.service.Usage(r.Context(), principal, workspaceID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": account, "usage": value})
}

func (h billingHandlers) checkout(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var in struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "plan_id is required")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	url, err := h.service.CreateCheckoutSession(r.Context(), principal, workspaceID, in.PlanID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": url})
}

func (h billingHandlers) portal(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	url, err := h.service.CreatePortalSession(r.Context(), principal, workspaceID)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": url})
}

func (h billingHandlers) webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid provider event")
		return
	}
	event, err := h.service.HandleProviderWebhook(r.Context(), chi.URLParam(r, "provider"), raw, r.Header.Get("X-Billing-Signature"))
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (h billingHandlers) serviceProviderBacked() bool { return h.service.ProviderName() != "fake" }

func (h billingHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if denial, ok := billing.AsBillingError(err); ok {
		writeAPIError(w, billing.HTTPStatus(denial.Code), denial.Code, denial.Message)
		return true
	}
	h.logger.Error("billing request failed", "error", err)
	writeDomainError(w, err)
	return true
}
