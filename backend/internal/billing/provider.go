package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

// BillingProvider is the narrow payment/subscription provider abstraction.
// Domain models never import provider SDK types; adapters translate.
type BillingProvider interface {
	Name() string
	CreateCheckoutSession(ctx context.Context, workspaceID, planID string) (url string, sessionRef string, err error)
	CreatePortalSession(ctx context.Context, workspaceID string) (url string, err error)
	// VerifyWebhook authenticates a provider callback before any state
	// mutation and returns the canonical event identity + type.
	VerifyWebhook(rawBody []byte, signature string) (eventID, eventType string, err error)
	// ParseEvent extracts safe reconciliation fields from a verified payload.
	ParseEvent(rawBody []byte) (ProviderReconciliation, error)
}

// ProviderReconciliation is the safe subset a provider event may reconcile.
// Stale/out-of-order events must never blindly overwrite newer truth;
// callers compare UpdatedAt against the account's UpdatedAt.
type ProviderReconciliation struct {
	EventID         string
	EventType       string
	WorkspaceID     string
	PlanID          string
	Status          string
	CustomerRef     string
	SubscriptionRef string
	UpdatedAt       time.Time
}

// FakeProvider is the deterministic test/development provider. It never
// touches the network and never handles real card data.
type FakeProvider struct{}

func (FakeProvider) Name() string { return "fake" }

func (FakeProvider) CreateCheckoutSession(_ context.Context, workspaceID, planID string) (string, string, error) {
	if !IsValidPlan(planID) {
		return "", "", fmt.Errorf("%w: unknown plan", identity.ErrValidation)
	}
	sessionRef := "fake_checkout_" + workspaceID + "_" + planID
	return "https://billing.local/fake/checkout?workspace=" + workspaceID + "&plan=" + planID, sessionRef, nil
}

func (FakeProvider) CreatePortalSession(_ context.Context, workspaceID string) (string, error) {
	return "https://billing.local/fake/portal?workspace=" + workspaceID, nil
}

// SignFakeEvent creates a deterministic test signature for the fake inbox.
func SignFakeEvent(secret string, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func (FakeProvider) VerifyWebhook(rawBody []byte, signature string) (string, string, error) {
	// Fake verification is performed by the service with the configured
	// secret; this adapter only extracts identity so verification stays in
	// one audited place. Signature presence is still required.
	if strings.TrimSpace(signature) == "" {
		return "", "", errors.New("missing webhook signature")
	}
	var payload struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return "", "", fmt.Errorf("%w: invalid webhook payload", identity.ErrValidation)
	}
	if strings.TrimSpace(payload.EventID) == "" || strings.TrimSpace(payload.EventType) == "" {
		return "", "", fmt.Errorf("%w: webhook event_id and event_type are required", identity.ErrValidation)
	}
	return payload.EventID, payload.EventType, nil
}

func (FakeProvider) ParseEvent(rawBody []byte) (ProviderReconciliation, error) {
	var payload struct {
		EventID         string `json:"event_id"`
		EventType       string `json:"event_type"`
		WorkspaceID     string `json:"workspace_id"`
		PlanID          string `json:"plan_id"`
		Status          string `json:"status"`
		CustomerRef     string `json:"customer_ref"`
		SubscriptionRef string `json:"subscription_ref"`
		UpdatedAt       string `json:"updated_at"`
	}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return ProviderReconciliation{}, fmt.Errorf("%w: invalid webhook payload", identity.ErrValidation)
	}
	out := ProviderReconciliation{
		EventID: payload.EventID, EventType: payload.EventType,
		WorkspaceID: payload.WorkspaceID, PlanID: payload.PlanID, Status: payload.Status,
		CustomerRef: payload.CustomerRef, SubscriptionRef: payload.SubscriptionRef,
		UpdatedAt: time.Now().UTC(),
	}
	if payload.UpdatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.UpdatedAt); err == nil {
			out.UpdatedAt = parsed.UTC()
		}
	}
	if out.WorkspaceID != "" {
		if err := identity.ValidateUUID(out.WorkspaceID); err != nil {
			return ProviderReconciliation{}, fmt.Errorf("%w: workspace_id must be a valid UUID", identity.ErrValidation)
		}
	}
	if out.PlanID != "" && !IsValidPlan(out.PlanID) {
		return ProviderReconciliation{}, fmt.Errorf("%w: unknown plan", identity.ErrValidation)
	}
	if out.Status != "" && !IsValidStatus(out.Status) {
		return ProviderReconciliation{}, fmt.Errorf("%w: unknown billing status", identity.ErrValidation)
	}
	return out, nil
}

// VerifySignature validates an HMAC-SHA256 webhook signature of the form
// "v1=<hex>" using constant-time comparison. Secrets come from environment,
// never from the database.
func VerifySignature(secret string, rawBody []byte, signature string) bool {
	signature = strings.TrimSpace(signature)
	if secret == "" || !strings.HasPrefix(signature, "v1=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := make([]byte, 64)
	hex.Encode(expected, mac.Sum(nil))
	provided := strings.TrimPrefix(signature, "v1=")
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), expected) == 1
}
