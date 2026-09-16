package environment

import (
	"context"
	"sort"
)

// SchedulerInput selects an execution placement.
type SchedulerInput struct {
	WorkspaceID  string
	Requirements Requirements
	// Purpose is trial or certification. Reserved for future policy; V1
	// scheduling is identical for both.
	Purpose string
	// ProfilePreference is an optional caller preference. It must be a known
	// server-owned active profile and must still satisfy requirements;
	// otherwise it is ignored with a reason code (authoritative paths
	// reject it outright).
	ProfilePreference string
	// AuthorizedMaxIsolation is the commercial ceiling supplied by the
	// billing domain. Empty means no additional commercial restriction.
	// A task is never downgraded to fit this ceiling.
	AuthorizedMaxIsolation string
}

// CandidateEvaluation is one profile/provider pair considered.
type CandidateEvaluation struct {
	ProfileID  string `json:"profile_id"`
	ProviderID string `json:"provider_id"`
	Eligible   bool   `json:"eligible"`
	ReasonCode string `json:"reason_code"`
	Detail     string `json:"detail,omitempty"`
}

// SchedulerDecision is the deterministic scheduler output.
type SchedulerDecision struct {
	Profile           *Profile              `json:"profile,omitempty"`
	Provider          *ProviderInfo         `json:"provider,omitempty"`
	ReasonCode        string                `json:"reason_code"`
	ReasonDetail      string                `json:"reason_detail,omitempty"`
	Candidates        []CandidateEvaluation `json:"candidates"`
	Requirements      Requirements          `json:"requirements"`
	SchedulerContract string                `json:"scheduler_contract_version"`
	RuntimeContract   string                `json:"runtime_contract_version,omitempty"`
}

// ProviderAvailability abstracts live health checks so tests use deterministic
// fakes and production wires the real registry.
type ProviderAvailability interface {
	Health(ctx context.Context, providerID string) ProviderHealth
	SupportsProfile(providerID, profileID string) bool
}

// staticAvailability is the default registry: kind-dev available, all future
// providers unavailable (planned only, no fake health).
type staticAvailability struct{}

func (staticAvailability) Health(_ context.Context, providerID string) ProviderHealth {
	if providerID == ProviderKindDev {
		return ProviderHealth{Available: true, Reason: "development provider healthy"}
	}
	return ProviderHealth{Available: false, Reason: "provider planned and not implemented"}
}

func (staticAvailability) SupportsProfile(providerID, profileID string) bool {
	info, ok := GetProvider(providerID)
	if !ok {
		return false
	}
	for _, supported := range info.SupportedProfiles {
		if supported == profileID {
			return true
		}
	}
	return false
}

// DefaultAvailability returns the V1 static registry.
func DefaultAvailability() ProviderAvailability { return staticAvailability{} }

// fakeAvailability is exported for focused tests.
type fakeAvailability struct {
	health   map[string]ProviderHealth
	supports map[string]map[string]bool
}

func (f fakeAvailability) Health(_ context.Context, providerID string) ProviderHealth {
	if h, ok := f.health[providerID]; ok {
		return h
	}
	return ProviderHealth{Available: false, Reason: "unknown provider"}
}

func (f fakeAvailability) SupportsProfile(providerID, profileID string) bool {
	return f.supports[providerID][profileID]
}

// NewFakeAvailability builds a deterministic test registry.
func NewFakeAvailability(health map[string]ProviderHealth, supports map[string]map[string]bool) ProviderAvailability {
	return fakeAvailability{health: health, supports: supports}
}

// Select deterministically chooses the least-fidelity adequate active
// profile/provider. Priorities: satisfy requirements → minimum isolation →
// provider available → least expensive/simplest adequate tier (profile
// priority weight). Never downgrades fidelity for availability.
func Select(ctx context.Context, in SchedulerInput, availability ProviderAvailability) SchedulerDecision {
	if availability == nil {
		availability = DefaultAvailability()
	}
	req := in.Requirements
	if len(req.RequiredComponents) == 0 {
		req = LegacyDefaultRequirements()
	}
	decision := SchedulerDecision{
		Requirements:      req,
		SchedulerContract: SchedulerContractVersion,
		RuntimeContract:   req.RuntimeContract,
		Candidates:        []CandidateEvaluation{},
	}
	if req.RuntimeContract != "" && !IsAllowedRuntimeContract(req.RuntimeContract) {
		decision.ReasonCode = ReasonRuntimeUnsupported
		decision.ReasonDetail = "unsupported runtime contract"
		return decision
	}
	profiles := ListProfiles()
	// Deterministic: priority weight then ID.
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Priority != profiles[j].Priority {
			return profiles[i].Priority < profiles[j].Priority
		}
		return profiles[i].ID < profiles[j].ID
	})
	providers := ListProviders()
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })

	preference := in.ProfilePreference
	if preference != "" {
		if _, ok := GetProfile(preference); !ok {
			decision.ReasonCode = ReasonNoEligibleEnvironment
			decision.ReasonDetail = "unknown profile preference"
			return decision
		}
	}

	type eligible struct {
		profile  Profile
		provider ProviderInfo
	}
	var eligibles []eligible
	for _, profile := range profiles {
		if preference != "" && profile.ID != preference {
			continue
		}
		if in.AuthorizedMaxIsolation != "" && IsolationRank(profile.Isolation) > IsolationRank(in.AuthorizedMaxIsolation) {
			for _, provider := range providers {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: "environment_tier_not_entitled", Detail: "profile isolation exceeds the authorized commercial maximum"})
			}
			continue
		}
		// Component gate.
		missing := ""
		for _, need := range req.RequiredComponents {
			if !profile.HasComponent(need) {
				missing = need
				break
			}
		}
		if missing != "" {
			for _, provider := range providers {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonMissingComponent, Detail: "profile lacks component " + missing})
			}
			continue
		}
		// Isolation gate: stronger may satisfy weaker; weaker never stronger.
		if !SatisfiesIsolation(profile.Isolation, req.MinimumIsolation) {
			for _, provider := range providers {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonInsufficientIsolation})
			}
			continue
		}
		// Status gate.
		if !profile.Executable() {
			for _, provider := range providers {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonProfileNotActive})
			}
			continue
		}
		// Runtime gate.
		if req.RuntimeContract != "" && profile.RuntimeContract != "" && profile.RuntimeContract != req.RuntimeContract {
			for _, provider := range providers {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonRuntimeUnsupported})
			}
			continue
		}
		matchedProvider := false
		for _, provider := range providers {
			if provider.Status != ProviderActive {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonProviderUnavailable, Detail: "provider not active"})
				continue
			}
			if !availability.SupportsProfile(provider.ID, profile.ID) {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonProviderUnavailable, Detail: "provider does not support profile"})
				continue
			}
			health := availability.Health(ctx, provider.ID)
			if !health.Available {
				decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: false, ReasonCode: ReasonProviderUnavailable, Detail: health.Reason})
				continue
			}
			matchedProvider = true
			decision.Candidates = append(decision.Candidates, CandidateEvaluation{ProfileID: profile.ID, ProviderID: provider.ID, Eligible: true, ReasonCode: ReasonProfileSelected})
			eligibles = append(eligibles, eligible{profile: profile, provider: provider})
		}
		_ = matchedProvider
	}
	if len(eligibles) == 0 {
		decision.ReasonCode = ReasonNoEligibleEnvironment
		if preference != "" {
			decision.ReasonCode = ReasonNoEligibleEnvironment
			decision.ReasonDetail = "preferred profile has no eligible provider"
		} else {
			decision.ReasonDetail = "no profile/provider satisfies requirements without fidelity downgrade"
		}
		return decision
	}
	best := eligibles[0]
	decision.Profile = &best.profile
	decision.Provider = &best.provider
	decision.ReasonCode = ReasonProfileSelected
	decision.RuntimeContract = best.profile.RuntimeContract
	if decision.RuntimeContract == "" {
		decision.RuntimeContract = req.RuntimeContract
	}
	return decision
}
