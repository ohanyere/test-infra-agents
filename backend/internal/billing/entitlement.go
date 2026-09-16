package billing

import "strings"

var stableLimits = map[int64]*int64{}

func init() {
	for _, value := range []int64{1, 2, 3, 5, 7, 10, 30, 50, 90, 365} {
		v := value
		stableLimits[value] = &v
	}
}

func stableLimit(value int64) *int64 {
	if limit, ok := stableLimits[value]; ok {
		return limit
	}
	v := value
	return &v
}

// CheckEntitlement evaluates one entitlement key against plan + billing
// status. It is pure and server-owned: callers cannot inject plan, status,
// or balance. RBAC is a separate gate evaluated before this function.
func CheckEntitlement(plan Plan, status, key string, opts CheckOptions) EntitlementDecision {
	base := EntitlementDecision{
		PlanID: plan.ID, PlanDisplayName: plan.DisplayName,
		BillingStatus:       status,
		EntitlementContract: EntitlementContractVersion,
		CatalogContract:     BillingCatalogContractVersion,
		Key:                 key,
	}
	// Commercial state gate: past_due/canceled/suspended block new expensive
	// execution but keep read paths available (enforced by callers).
	if status == StatusPastDue || status == StatusCanceled || status == StatusSuspended {
		code := CodeBillingPastDue
		reason := "billing action required before starting new evaluations"
		if status == StatusCanceled {
			code = CodeBillingCanceled
			reason = "subscription canceled: new evaluations are blocked, historical reads remain available"
		}
		if status == StatusSuspended {
			code = CodeBillingSuspended
			reason = "workspace suspended: new evaluations are blocked, historical reads remain available"
		}
		if key == EntCanRunEvaluations || key == EntMaxParallelTrials {
			base.Allowed = false
			base.Reason = reason
			base.Code = code
			return base
		}
	}
	switch key {
	case EntCanRunEvaluations:
		base.Allowed = plan.CanRunEvaluations
		base.Reason = allowanceReason(plan.CanRunEvaluations, "evaluation execution")
	case EntCanBuildQualificationSuites:
		base.Allowed = plan.CanBuildQualificationSuites
		base.Reason = allowanceReason(plan.CanBuildQualificationSuites, "qualification suite authoring")
	case EntCanUseAPI:
		base.Allowed = plan.CanUseAPI
		base.Reason = allowanceReason(plan.CanUseAPI, "API access")
	case EntCanUseMCP:
		base.Allowed = plan.CanUseMCP
		base.Reason = allowanceReason(plan.CanUseMCP, "MCP invocation")
	case EntCanUseIncidentImports:
		base.Allowed = plan.CanUseIncidentImports
		base.Reason = allowanceReason(plan.CanUseIncidentImports, "incident imports")
		if !base.Allowed {
			base.Code = CodeEntitlementDenied
		}
	case EntCanUseAdvancedReliability:
		base.Allowed = plan.CanUseAdvancedReliability
		base.Reason = allowanceReason(plan.CanUseAdvancedReliability, "advanced reliability")
		if !base.Allowed {
			base.Code = CodeEntitlementDenied
		}
	case EntCanUseGitOpsQualification:
		base.Allowed = plan.CanUseGitOpsQualification && opts.RuntimeExists
		if !plan.CanUseGitOpsQualification {
			base.Reason = "gitops qualification is not included in this plan"
			base.Code = CodeEntitlementDenied
		} else if !opts.RuntimeExists {
			base.Reason = "gitops runtime is not available; nothing is billed until it ships"
			base.Code = CodeEntitlementDenied
		} else {
			base.Reason = "gitops qualification entitled while runtime exists"
		}
	case EntAllowedEnvironmentIsolation:
		required := opts.RequiredIsolation
		if required == "" {
			required = IsolationNamespace
		}
		base.Value = plan.MaxIsolation
		base.LimitLabel = plan.MaxIsolation
		if IsolationRank(required) < 0 {
			base.Allowed = false
			base.Reason = "unknown isolation tier requested"
			base.Code = CodeEnvironmentTierNotEntitled
			return base
		}
		if IsolationAllowed(plan.MaxIsolation, required) {
			base.Allowed = true
			base.Reason = "required isolation " + required + " is within authorized maximum " + plan.MaxIsolation
		} else {
			base.Allowed = false
			base.Reason = "dedicated environments are not included in this plan; required isolation " + required + " exceeds authorized maximum " + plan.MaxIsolation
			base.Code = CodeEnvironmentTierNotEntitled
		}
	case EntMaxParallelTrials:
		base.Allowed = true
		base.Limit = stableLimit(int64(plan.MaxParallelTrials))
		base.LimitLabel = itoa(plan.MaxParallelTrials)
		base.Reason = "plan allows up to " + itoa(plan.MaxParallelTrials) + " parallel trials"
	case EntMaxWorkspaceMembers:
		if plan.MaxMembers < 0 {
			base.Allowed = true
			base.LimitLabel = "unlimited"
			base.Reason = "enterprise custom member allowance"
		} else {
			base.Allowed = true
			base.Limit = stableLimit(int64(plan.MaxMembers))
			base.LimitLabel = itoa(plan.MaxMembers)
			base.Reason = "plan allows up to " + itoa(plan.MaxMembers) + " workspace members"
		}
	case EntEvidenceRetentionDays:
		if plan.EvidenceRetentionDays == 0 {
			base.Allowed = true
			base.LimitLabel = "custom"
			base.Reason = "enterprise custom retention policy; nothing is deleted in this stage"
		} else {
			base.Allowed = true
			base.Limit = stableLimit(int64(plan.EvidenceRetentionDays))
			base.LimitLabel = itoa(plan.EvidenceRetentionDays) + " days"
			base.Reason = "evidence retention policy is " + itoa(plan.EvidenceRetentionDays) + " days; deletion remains deferred"
		}
	default:
		base.Allowed = false
		base.Reason = "unknown entitlement key"
		base.Code = CodeEntitlementDenied
	}
	if !base.Allowed && base.Code == "" && (key == EntCanRunEvaluations || key == EntCanBuildQualificationSuites || key == EntCanUseAPI || key == EntCanUseMCP) {
		base.Code = CodeEntitlementDenied
	}
	return base
}

// CheckOptions carries request-scoped pins for entitlement evaluation.
type CheckOptions struct {
	RuntimeExists     bool
	RequiredIsolation string
}

// AllEntitlements returns the full V1 entitlement snapshot for a workspace.
func AllEntitlements(plan Plan, status string, runtimeExists bool, requiredIsolation string) []EntitlementDecision {
	keys := []string{
		EntCanRunEvaluations, EntCanBuildQualificationSuites, EntCanUseAPI, EntCanUseMCP,
		EntCanUseIncidentImports, EntCanUseAdvancedReliability, EntCanUseGitOpsQualification,
		EntAllowedEnvironmentIsolation, EntMaxParallelTrials, EntMaxWorkspaceMembers, EntEvidenceRetentionDays,
	}
	out := make([]EntitlementDecision, 0, len(keys))
	for _, key := range keys {
		out = append(out, CheckEntitlement(plan, status, key, CheckOptions{RuntimeExists: runtimeExists, RequiredIsolation: requiredIsolation}))
	}
	return out
}

func allowanceReason(allowed bool, what string) string {
	if allowed {
		return what + " is included in this plan"
	}
	return what + " is not included in this plan"
}

func itoa(v int) string {
	if v < 0 {
		return "unlimited"
	}
	digits := "0123456789"
	if v == 0 {
		return "0"
	}
	out := ""
	for v > 0 {
		out = string(digits[v%10]) + out
		v /= 10
	}
	return out
}

// DenialMessage renders a user-facing denial from a decision without leaking
// internals.
func DenialMessage(d EntitlementDecision) string {
	switch d.Code {
	case CodeInsufficientCredits:
		return "Insufficient evaluation credits"
	case CodePlanLimitReached:
		return "Plan limit reached"
	case CodeEnvironmentTierNotEntitled:
		return "Dedicated environments are not included in this plan"
	case CodeBillingPastDue:
		return "Billing action required"
	case CodeBillingCanceled:
		return "Subscription canceled: historical reads remain available"
	case CodeBillingSuspended:
		return "Workspace suspended: contact support"
	default:
		if d.Reason != "" {
			return d.Reason
		}
		return "Operation not entitled on this plan"
	}
}

// NormalizePlanID lowercases and trims caller input for lookup only; plan
// identity on persisted records always uses the stable catalog ID.
func NormalizePlanID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
