package billing

import (
	"errors"
	"fmt"
)

// BillingError is a structured, machine-readable denial. Code is one of the
// Code* denial codes; HTTP mapping lives in httpapi (402 for payment action
// required, 403 for entitlement denials).
type BillingError struct {
	Code    string
	Message string
}

func (e *BillingError) Error() string { return e.Code + ": " + e.Message }

// AsBillingError extracts a *BillingError from err.
func AsBillingError(err error) (*BillingError, bool) {
	var target *BillingError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func entitlementDenied(code, message string) *BillingError {
	return &BillingError{Code: code, Message: message}
}

// HTTPStatus maps a denial code to an HTTP status. Payment-action-required
// states use 402; entitlement/RBAC-style denials use 403.
func HTTPStatus(code string) int {
	switch code {
	case CodeInsufficientCredits, CodeBillingPastDue:
		return 402
	default:
		return 403
	}
}

func checkPlanID(planID string) error {
	if !IsValidPlan(planID) {
		return fmt.Errorf("unknown plan %q", planID)
	}
	return nil
}
