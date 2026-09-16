package billing

// Stable plan identities. Display names and prices are catalog metadata and
// must never be used as identity.
const (
	PlanDeveloper    = "developer"
	PlanStarterID    = "starter"
	PlanPro          = "pro"
	PlanOrganization = "organization"
	PlanEnterprise   = "enterprise"
)

// Billing statuses. Subscription state is separate from product entitlements.
const (
	StatusFree      = "free"
	StatusTrialing  = "trialing"
	StatusActive    = "active"
	StatusPastDue   = "past_due"
	StatusCanceled  = "canceled"
	StatusSuspended = "suspended"
)

// Entitlement keys (entitlement-contract:v1).
const (
	EntCanRunEvaluations           = "can_run_evaluations"
	EntCanBuildQualificationSuites = "can_build_qualification_suites"
	EntCanUseAPI                   = "can_use_api"
	EntCanUseMCP                   = "can_use_mcp"
	EntCanUseIncidentImports       = "can_use_incident_imports"
	EntCanUseAdvancedReliability   = "can_use_advanced_reliability"
	EntCanUseGitOpsQualification   = "can_use_gitops_qualification_when_runtime_exists"
	EntAllowedEnvironmentIsolation = "allowed_environment_isolation"
	EntMaxParallelTrials           = "max_parallel_trials"
	EntMaxWorkspaceMembers         = "max_workspace_members"
	EntEvidenceRetentionDays       = "evidence_retention_days"
)

// Machine-readable denial codes (entitlement-contract:v1).
const (
	CodeEntitlementDenied          = "entitlement_denied"
	CodePlanLimitReached           = "plan_limit_reached"
	CodeInsufficientCredits        = "insufficient_credits"
	CodeBillingPastDue             = "billing_past_due"
	CodeEnvironmentTierNotEntitled = "environment_tier_not_entitled"
	CodeBillingSuspended           = "billing_suspended"
	CodeBillingCanceled            = "billing_canceled"
)

// Isolation tiers mirror the environment contract ordering:
// namespace < virtual_cluster < dedicated_cluster.
const (
	IsolationNamespace        = "namespace"
	IsolationVirtualCluster   = "virtual_cluster"
	IsolationDedicatedCluster = "dedicated_cluster"
)

// Plan describes one catalog entry. MonthlyCredits of -1 means custom /
// effectively unlimited (Enterprise volume). MaxMembers of -1 means unlimited.
// RetentionDays of 0 means custom retention (Enterprise).
type Plan struct {
	ID                    string `json:"id"`
	DisplayName           string `json:"display_name"`
	PriceCents            int64  `json:"price_cents"`
	PriceDisplay          string `json:"price_display"`
	Cadence               string `json:"cadence"`
	MonthlyCredits        int64  `json:"monthly_credits"`
	MaxMembers            int    `json:"max_members"`
	MaxParallelTrials     int    `json:"max_parallel_trials"`
	EvidenceRetentionDays int    `json:"evidence_retention_days"`
	MaxIsolation          string `json:"max_isolation"`

	CanRunEvaluations           bool `json:"can_run_evaluations"`
	CanBuildQualificationSuites bool `json:"can_build_qualification_suites"`
	CanUseAPI                   bool `json:"can_use_api"`
	CanUseMCP                   bool `json:"can_use_mcp"`
	CanUseIncidentImports       bool `json:"can_use_incident_imports"`
	CanUseAdvancedReliability   bool `json:"can_use_advanced_reliability"`
	CanUseGitOpsQualification   bool `json:"can_use_gitops_qualification_when_runtime_exists"`
}

// Catalog returns the server-owned V1 plan catalog. Values are derived from
// the public /pricing page: Developer $0, Starter $49, Pro $299,
// Organization $999, Enterprise custom. Credit allowances match published
// copy (10 / 100 / 500 / 2,000). No annual discounts, no overage prices.
func Catalog() []Plan {
	return []Plan{
		{
			ID: PlanDeveloper, DisplayName: "Developer",
			PriceCents: 0, PriceDisplay: "$0", Cadence: "free",
			MonthlyCredits: 10, MaxMembers: 1, MaxParallelTrials: 1,
			EvidenceRetentionDays: 7, MaxIsolation: IsolationNamespace,
			CanRunEvaluations: true, CanBuildQualificationSuites: true,
			CanUseAPI: true, CanUseMCP: true,
			CanUseIncidentImports: false, CanUseAdvancedReliability: false,
			CanUseGitOpsQualification: false,
		},
		{
			ID: PlanStarterID, DisplayName: "Starter",
			PriceCents: 4900, PriceDisplay: "$49", Cadence: "/ month",
			MonthlyCredits: 100, MaxMembers: 3, MaxParallelTrials: 2,
			EvidenceRetentionDays: 30, MaxIsolation: IsolationNamespace,
			CanRunEvaluations: true, CanBuildQualificationSuites: true,
			CanUseAPI: true, CanUseMCP: true,
			CanUseIncidentImports: false, CanUseAdvancedReliability: false,
			CanUseGitOpsQualification: false,
		},
		{
			ID: PlanPro, DisplayName: "Pro",
			PriceCents: 29900, PriceDisplay: "$299", Cadence: "/ month",
			MonthlyCredits: 500, MaxMembers: 10, MaxParallelTrials: 5,
			EvidenceRetentionDays: 90, MaxIsolation: IsolationNamespace,
			CanRunEvaluations: true, CanBuildQualificationSuites: true,
			CanUseAPI: true, CanUseMCP: true,
			CanUseIncidentImports: true, CanUseAdvancedReliability: true,
			CanUseGitOpsQualification: true,
		},
		{
			ID: PlanOrganization, DisplayName: "Organization",
			PriceCents: 99900, PriceDisplay: "$999", Cadence: "/ month",
			MonthlyCredits: 2000, MaxMembers: 50, MaxParallelTrials: 10,
			EvidenceRetentionDays: 365, MaxIsolation: IsolationNamespace,
			CanRunEvaluations: true, CanBuildQualificationSuites: true,
			CanUseAPI: true, CanUseMCP: true,
			CanUseIncidentImports: true, CanUseAdvancedReliability: true,
			CanUseGitOpsQualification: true,
		},
		{
			ID: PlanEnterprise, DisplayName: "Enterprise",
			PriceCents: -1, PriceDisplay: "Custom", Cadence: "",
			MonthlyCredits: -1, MaxMembers: -1, MaxParallelTrials: 50,
			EvidenceRetentionDays: 0, MaxIsolation: IsolationDedicatedCluster,
			CanRunEvaluations: true, CanBuildQualificationSuites: true,
			CanUseAPI: true, CanUseMCP: true,
			CanUseIncidentImports: true, CanUseAdvancedReliability: true,
			CanUseGitOpsQualification: true,
		},
	}
}

// GetPlan returns the catalog entry for a stable plan ID.
func GetPlan(planID string) (Plan, bool) {
	for _, p := range Catalog() {
		if p.ID == planID {
			return p, true
		}
	}
	return Plan{}, false
}

// IsValidPlan reports whether planID is a known stable plan identity.
func IsValidPlan(planID string) bool {
	_, ok := GetPlan(planID)
	return ok
}

// IsValidStatus reports whether status is a known billing status.
func IsValidStatus(status string) bool {
	switch status {
	case StatusFree, StatusTrialing, StatusActive, StatusPastDue, StatusCanceled, StatusSuspended:
		return true
	default:
		return false
	}
}

// IsolationRank orders fidelity. Unknown tiers return -1 and never match.
func IsolationRank(tier string) int {
	switch tier {
	case IsolationNamespace:
		return 0
	case IsolationVirtualCluster:
		return 1
	case IsolationDedicatedCluster:
		return 2
	default:
		return -1
	}
}

// IsolationAllowed reports whether required isolation is within the plan's
// authorized maximum. It never downgrades: a weaker plan tier cannot satisfy
// a stronger requirement.
func IsolationAllowed(planMax, required string) bool {
	max, need := IsolationRank(planMax), IsolationRank(required)
	if max < 0 || need < 0 {
		return false
	}
	return max >= need
}
