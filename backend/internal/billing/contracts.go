// Package billing owns Evalplane's server-side commercial-control layer.
//
// Contracts:
//
//	billing-catalog-contract:v1 — plan identities, display metadata, prices,
//	  monthly allowances, and the V1 entitlement matrix.
//	entitlement-contract:v1 — entitlement keys, decision shape, denial codes.
//	usage-meter-contract:v1 — billable unit (evaluation execution usage units),
//	  raw-usage pins, and reservation → commit/release/refund lifecycle.
//
// The payment provider is never the authorization source of truth. Internal
// billing state (plan, status, entitlements, usage ledger) authorizes
// execution; provider events only reconcile subscription state.
//
// No LLM is used anywhere in this package. All decisions are deterministic.
package billing

const (
	// BillingCatalogContractVersion pins plan catalog semantics.
	BillingCatalogContractVersion = "billing-catalog-contract:v1"
	// EntitlementContractVersion pins entitlement decision semantics.
	EntitlementContractVersion = "entitlement-contract:v1"
	// UsageMeterContractVersion pins usage metering semantics.
	UsageMeterContractVersion = "usage-meter-contract:v1"
)
