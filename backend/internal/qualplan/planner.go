package qualplan

import (
	"sort"

	"github.com/evalplane/evalplane/backend/internal/competency"
	"github.com/evalplane/evalplane/backend/internal/coverage"
)

// RequirementView is the coverage rule for one capability. The service
// builds it from coverage.GetRequirementV2 so V1 semantics stay pinned.
type RequirementView struct {
	CapabilityID            string
	RequiredFamilies        []string
	MinDistinctFamilies     int
	MinValidTrialsPerFamily int
	RequiredCaseTypes       []string
	RequiredCaseTypeGroups  [][]string
	ContractVersion         string
}

// RequirementFor converts a coverage requirement into a planner view.
func RequirementFor(requirement coverage.CoverageRequirement) RequirementView {
	return RequirementView{
		CapabilityID:            requirement.CapabilityID,
		RequiredFamilies:        append([]string(nil), requirement.RequiredFamilies...),
		MinDistinctFamilies:     requirement.MinDistinctFamilies,
		MinValidTrialsPerFamily: requirement.MinValidTrialsPerFamily,
		RequiredCaseTypes:       append([]string(nil), requirement.RequiredCaseTypes...),
		RequiredCaseTypeGroups:  append([][]string(nil), requirement.RequiredCaseTypeGroups...),
		ContractVersion:         requirement.ContractVersion,
	}
}

// Candidate is one exact task version that could satisfy coverage. The
// service derives it from the built-in catalog bridge (plus workspace
// scenario versions carrying explicit family assignments), fresh Stage-28
// certification status, Stage-29 scheduler truth, and runtime compatibility.
type Candidate struct {
	BuiltinTaskID     string
	ScenarioID        string
	ScenarioVersionID string
	FamilyID          string
	CaseType          string
	Capabilities      []string
	RuntimeContract   string
	RuntimeAllowed    bool
	// Requires lists low-level runtime capabilities the task needs
	// (gateway action vocabulary). Compared against the AgentVersion's
	// declared runtime capabilities; missing entries are never granted.
	Requires []string
	// AgentCompatibleValue is the service-computed per-task compatibility
	// verdict (agent declares every entry of Requires). The pure planner
	// consumes it; the service always populates it, so this stays a real
	// gate rather than a formality.
	AgentCompatibleValue bool
	// Certified is true only for fresh-certified authoritative evidence.
	Certified bool
	// CertStale is true when a certification exists but is stale.
	CertStale bool
	// CertificationID identifies the latest certification record behind the
	// eligibility verdict ("" when no attempt exists). Persisted on
	// selections so old plans never depend on mutable current state.
	CertificationID string
	// EnvExecutable is true when the scheduler finds an eligible
	// server-owned profile/provider without fidelity downgrade.
	EnvExecutable bool
	EnvProfile    string
	EnvReason     string
	// EnvComponents and EnvIsolation pin the task's environment
	// requirements on the selection for reproducibility.
	EnvComponents []string
	EnvIsolation  string
	ContentHash   string
}

// PlannerInput is the full deterministic planning input. Same input must
// always produce the same plan: no LLM, no wall-clock, no randomness.
type PlannerInput struct {
	AgentVersionID  string
	DeclaredRuntime []string
	// Claimed is the selected capability subset (already validated to be
	// within the AgentVersion's declared claims).
	Claimed      []string
	Requirements map[string]RequirementView
	Candidates   []Candidate
	PolicyAvail  bool
}

// PlanOutcome is the pure planner result before persistence.
type PlanOutcome struct {
	State         string
	CapabilityIDs []string
	Capabilities  []CapabilityPlan
	Blockers      []Blocker
	PolicyAvail   bool
}

// BuildPlan deterministically derives what must be tested for the claimed
// capabilities. Pure function: no I/O, no LLM, stable sorted output.
func BuildPlan(input PlannerInput) PlanOutcome {
	claimed := sortedUnique(input.Claimed)
	out := PlanOutcome{
		CapabilityIDs: claimed,
		Capabilities:  []CapabilityPlan{},
		Blockers:      []Blocker{},
		PolicyAvail:   input.PolicyAvail,
	}
	if len(claimed) == 0 {
		out.State = StateBlocked
		out.Blockers = []Blocker{{
			Code:        BlockerNoCapabilityClaims,
			Detail:      "agent version declares no competency capability claims; nothing can be qualified",
			BlocksSuite: true,
		}}
		return out
	}
	satisfied := 0
	for _, capabilityID := range claimed {
		capability := planCapability(capabilityID, input)
		if capability.Satisfied {
			satisfied++
		}
		out.Capabilities = append(out.Capabilities, capability)
		for _, blocker := range capability.Blockers {
			if blocker.BlocksSuite {
				out.Blockers = append(out.Blockers, blocker)
			}
		}
	}
	switch {
	case satisfied == len(claimed):
		out.State = StateReady
	case satisfied > 0:
		out.State = StatePartial
	default:
		out.State = StateBlocked
	}
	if !input.PolicyAvail {
		out.Blockers = append(out.Blockers, Blocker{
			Code:        BlockerQualificationPolicyUnavailable,
			Detail:      "workspace has no qualification policy; resulting evidence cannot yet produce a qualification verdict",
			BlocksSuite: false,
		})
	}
	sortBlockers(out.Blockers)
	return out
}

// planCapability derives the per-capability outcome: coverage requirement ->
// required families/case groups -> eligible candidates -> minimal selection.
func planCapability(capabilityID string, input PlannerInput) CapabilityPlan {
	plan := CapabilityPlan{
		CapabilityID:    capabilityID,
		SelectedTasks:   []SelectedTask{},
		CoveredFamilies: []string{},
		Blockers:        []Blocker{},
	}
	requirement, ok := input.Requirements[capabilityID]
	if !ok {
		plan.CoverageContract = CoverageContractVersion
		plan.Blockers = []Blocker{{
			Code:         BlockerCoverageRequirementUnavailable,
			CapabilityID: capabilityID,
			Detail:       "no published coverage requirement for this capability under " + CoverageContractVersion,
			BlocksSuite:  true,
		}}
		return plan
	}
	plan.CoverageContract = requirement.ContractVersion
	plan.RequiredFamilies = append([]string(nil), requirement.RequiredFamilies...)
	plan.MinDistinctFamilies = requirement.MinDistinctFamilies

	// Claims are the source, but runtime compatibility is still verified
	// before READY: missing tool permissions are never auto-granted.
	if missing := competency.MissingRequirements(input.DeclaredRuntime, []string{capabilityID}); len(missing) > 0 {
		plan.Blockers = append(plan.Blockers, Blocker{
			Code:         BlockerAgentRuntimeIncompatible,
			CapabilityID: capabilityID,
			Detail:       "agent version lacks required runtime capabilities for this claim: " + joinSorted(missing[0].Missing),
			BlocksSuite:  true,
		})
		return plan
	}

	pool := candidatesForCapability(input.Candidates, capabilityID)
	plan.EligibleCandidates = countEligible(pool)
	byFamily := groupByFamily(pool)

	// Per-family minimal selection: the lowest-ID eligible task first.
	selectedByFamily := map[string][]Candidate{}
	familyBlocked := map[string]bool{}
	for _, familyID := range requirement.RequiredFamilies {
		familyPool := byFamily[familyID]
		if len(familyPool) == 0 {
			plan.Blockers = append(plan.Blockers, Blocker{
				Code:         BlockerRequiredTaskFamilyUnavailable,
				CapabilityID: capabilityID,
				FamilyID:     familyID,
				Detail:       "no task in the catalog belongs to required task family " + familyID,
				BlocksSuite:  true,
			})
			familyBlocked[familyID] = true
			continue
		}
		eligible := eligibleIn(familyPool)
		if len(eligible) == 0 {
			for _, blocker := range ineligibilityBlockers(capabilityID, familyID, familyPool) {
				plan.Blockers = append(plan.Blockers, blocker)
			}
			familyBlocked[familyID] = true
			continue
		}
		sortCandidates(eligible)
		selectedByFamily[familyID] = []Candidate{eligible[0]}
	}

	// Scope-wide case-type coverage (mirrors coverage.Assess: required case
	// types and OR-groups are satisfied anywhere in scope, not per family).
	// Greedily add the lowest-ID eligible task that closes a gap so contrast
	// requirements are preserved with a minimal set.
	selected := flattenSelection(requirement.RequiredFamilies, selectedByFamily)
	for {
		missingTypes, missingGroups := caseTypeGaps(requirement, selected)
		if len(missingTypes) == 0 && len(missingGroups) == 0 {
			break
		}
		added := false
		for _, familyID := range requirement.RequiredFamilies {
			if familyBlocked[familyID] {
				continue
			}
			for _, candidate := range byFamily[familyID] {
				if !isEligible(candidate) || containsCandidate(selected, candidate) {
					continue
				}
				if closesGap(candidate.CaseType, missingTypes, missingGroups) {
					selectedByFamily[familyID] = append(selectedByFamily[familyID], candidate)
					sortCandidates(selectedByFamily[familyID])
					selected = flattenSelection(requirement.RequiredFamilies, selectedByFamily)
					added = true
					break
				}
			}
			if added {
				break
			}
		}
		if !added {
			break
		}
	}
	missingTypes, missingGroups := caseTypeGaps(requirement, selected)
	for _, caseType := range missingTypes {
		plan.Blockers = append(plan.Blockers, Blocker{
			Code:         BlockerRequiredCaseTypeUnavailable,
			CapabilityID: capabilityID,
			Detail:       "no eligible task exercises required case type: " + caseType,
			BlocksSuite:  true,
		})
	}
	for _, group := range missingGroups {
		plan.Blockers = append(plan.Blockers, Blocker{
			Code:         BlockerRequiredCaseTypeUnavailable,
			CapabilityID: capabilityID,
			Detail:       "no eligible task exercises required case-type group: one of " + joinSorted(group),
			BlocksSuite:  true,
		})
	}

	// Trial counts: distribute each family's minimum across its selected
	// tasks (at least one trial each, remainder to the first in sort order).
	// Family totals always equal the requirement minimum: minimal sufficient.
	selections := []SelectedTask{}
	covered := []string{}
	for _, familyID := range requirement.RequiredFamilies {
		tasks := selectedByFamily[familyID]
		if len(tasks) == 0 {
			continue
		}
		trials := distributeTrials(requirement.MinValidTrialsPerFamily, len(tasks))
		for index, candidate := range tasks {
			selections = append(selections, SelectedTask{
				BuiltinTaskID:      candidate.BuiltinTaskID,
				ScenarioID:         candidate.ScenarioID,
				ScenarioVersionID:  candidate.ScenarioVersionID,
				FamilyID:           familyID,
				CaseType:           candidate.CaseType,
				TrialCount:         trials[index],
				ContentHash:        candidate.ContentHash,
				EnvironmentProfile: candidate.EnvProfile,
				CertificationID:    candidate.CertificationID,
				RequiredComponents: append([]string(nil), candidate.EnvComponents...),
				MinimumIsolation:   candidate.EnvIsolation,
			})
		}
		covered = append(covered, familyID)
	}
	sortSelections(selections)
	for index := range selections {
		selections[index].SortOrder = index
	}
	plan.SelectedTasks = selections
	plan.CoveredFamilies = covered

	if len(covered) < requirement.MinDistinctFamilies {
		plan.Blockers = append(plan.Blockers, Blocker{
			Code:         BlockerInsufficientIndependentFamiles,
			CapabilityID: capabilityID,
			Detail:       "fewer independent task families available than required",
			BlocksSuite:  true,
		})
	}
	eligibleTotal := 0
	for _, familyID := range requirement.RequiredFamilies {
		eligibleTotal += len(eligibleIn(byFamily[familyID]))
	}
	if len(covered) >= requirement.MinDistinctFamilies && eligibleTotal == 0 {
		plan.Blockers = append(plan.Blockers, Blocker{
			Code:         BlockerInsufficientValidCandidates,
			CapabilityID: capabilityID,
			Detail:       "no eligible task candidates satisfy the coverage requirement",
			BlocksSuite:  true,
		})
	}
	plan.Satisfied = len(covered) >= requirement.MinDistinctFamilies &&
		len(missingTypes) == 0 && len(missingGroups) == 0 &&
		len(plan.Blockers) == 0
	return plan
}

// candidatesForCapability selects catalog tasks whose metadata genuinely
// names the capability. One Task Family never masquerades as another:
// family identity is exact.
func candidatesForCapability(candidates []Candidate, capabilityID string) []Candidate {
	out := []Candidate{}
	for _, candidate := range candidates {
		for _, id := range candidate.Capabilities {
			if id == capabilityID {
				out = append(out, candidate)
				break
			}
		}
	}
	return out
}

// isEligible reports whether a candidate may satisfy an authoritative plan:
// fresh-certified, environment-executable, runtime-supported, and
// agent-compatible. All four must hold.
func isEligible(candidate Candidate) bool {
	return candidate.Certified && candidate.EnvExecutable && candidate.RuntimeAllowed && agentCompatible(candidate)
}

// agentCompatible consumes the service-computed per-task verdict.
func agentCompatible(candidate Candidate) bool {
	return candidate.AgentCompatibleValue
}

// eligibleIn returns the eligible subset of a family pool.
func eligibleIn(pool []Candidate) []Candidate {
	out := []Candidate{}
	for _, candidate := range pool {
		if isEligible(candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// countEligible counts eligible candidates across families.
func countEligible(pool []Candidate) int {
	count := 0
	for _, candidate := range pool {
		if isEligible(candidate) {
			count++
		}
	}
	return count
}

// ineligibilityBlockers explains why a family with candidates still has no
// eligible task. One blocker per failing category (deterministic order:
// runtime contract, agent compatibility, certification, environment).
func ineligibilityBlockers(capabilityID, familyID string, pool []Candidate) []Blocker {
	seen := map[string]bool{}
	out := []Blocker{}
	emit := func(code, detail string) {
		if seen[code] {
			return
		}
		seen[code] = true
		out = append(out, Blocker{Code: code, CapabilityID: capabilityID, FamilyID: familyID, Detail: detail, BlocksSuite: true})
	}
	sortCandidates(pool)
	for _, candidate := range pool {
		switch {
		case !candidate.RuntimeAllowed:
			emit(BlockerRuntimeContractUnsupported, "task runtime contract is not supported: "+candidate.RuntimeContract)
		case !agentCompatible(candidate):
			emit(BlockerAgentRuntimeIncompatible, "agent version lacks tool permissions required by task "+candidate.BuiltinTaskID)
		case candidate.CertStale:
			emit(BlockerTaskCertificationStale, "task certification is stale for "+candidate.BuiltinTaskID)
		case !candidate.Certified:
			emit(BlockerTaskNotCertified, "task "+candidate.BuiltinTaskID+" lacks fresh authoritative certification")
		case !candidate.EnvExecutable:
			reason := candidate.EnvReason
			if reason == "" {
				reason = "no eligible environment"
			}
			emit(BlockerEnvironmentUnavailable, "task "+candidate.BuiltinTaskID+" is not environment-executable: "+reason)
		}
	}
	return out
}

// caseTypeGaps returns required case types and groups not yet exercised by
// the selected tasks (scope-wide, mirroring coverage.Assess).
func caseTypeGaps(requirement RequirementView, selected []Candidate) ([]string, [][]string) {
	exercised := map[string]bool{}
	for _, candidate := range selected {
		if candidate.CaseType != "" {
			exercised[candidate.CaseType] = true
		}
	}
	missingTypes := []string{}
	for _, required := range requirement.RequiredCaseTypes {
		if !exercised[required] {
			missingTypes = append(missingTypes, required)
		}
	}
	missingGroups := [][]string{}
	for _, group := range requirement.RequiredCaseTypeGroups {
		satisfied := false
		for _, member := range group {
			if exercised[member] {
				satisfied = true
				break
			}
		}
		if !satisfied {
			missingGroups = append(missingGroups, group)
		}
	}
	sort.Strings(missingTypes)
	return missingTypes, missingGroups
}

// closesGap reports whether a case type helps satisfy outstanding gaps.
func closesGap(caseType string, missingTypes []string, missingGroups [][]string) bool {
	for _, required := range missingTypes {
		if caseType == required {
			return true
		}
	}
	for _, group := range missingGroups {
		for _, member := range group {
			if caseType == member {
				return true
			}
		}
	}
	return false
}

// distributeTrials splits a family minimum across n tasks: at least one
// trial each, remainder to the first in sort order. Total always equals the
// minimum: minimal but sufficient.
func distributeTrials(minimum, n int) []int {
	if n <= 0 {
		return []int{}
	}
	trials := make([]int, n)
	for i := range trials {
		trials[i] = 1
	}
	remaining := minimum - n
	for remaining > 0 {
		trials[0]++
		remaining--
	}
	return trials
}

func groupByFamily(pool []Candidate) map[string][]Candidate {
	out := map[string][]Candidate{}
	for _, candidate := range pool {
		out[candidate.FamilyID] = append(out[candidate.FamilyID], candidate)
	}
	return out
}

func flattenSelection(order []string, selectedByFamily map[string][]Candidate) []Candidate {
	out := []Candidate{}
	for _, familyID := range order {
		out = append(out, selectedByFamily[familyID]...)
	}
	return out
}

func containsCandidate(selected []Candidate, candidate Candidate) bool {
	for _, item := range selected {
		if item.ScenarioVersionID == candidate.ScenarioVersionID {
			return true
		}
	}
	return false
}

// sortCandidates orders by family, then built-in task ID, then exact
// scenario version ID: explicit priority then stable IDs.
func sortCandidates(candidates []Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].FamilyID != candidates[j].FamilyID {
			return candidates[i].FamilyID < candidates[j].FamilyID
		}
		if candidates[i].BuiltinTaskID != candidates[j].BuiltinTaskID {
			return candidates[i].BuiltinTaskID < candidates[j].BuiltinTaskID
		}
		return candidates[i].ScenarioVersionID < candidates[j].ScenarioVersionID
	})
}

func sortSelections(selections []SelectedTask) {
	sort.Slice(selections, func(i, j int) bool {
		if selections[i].FamilyID != selections[j].FamilyID {
			return selections[i].FamilyID < selections[j].FamilyID
		}
		if selections[i].BuiltinTaskID != selections[j].BuiltinTaskID {
			return selections[i].BuiltinTaskID < selections[j].BuiltinTaskID
		}
		return selections[i].ScenarioVersionID < selections[j].ScenarioVersionID
	})
}

func sortBlockers(blockers []Blocker) {
	sort.Slice(blockers, func(i, j int) bool {
		if blockers[i].Code != blockers[j].Code {
			return blockers[i].Code < blockers[j].Code
		}
		if blockers[i].CapabilityID != blockers[j].CapabilityID {
			return blockers[i].CapabilityID < blockers[j].CapabilityID
		}
		if blockers[i].FamilyID != blockers[j].FamilyID {
			return blockers[i].FamilyID < blockers[j].FamilyID
		}
		return blockers[i].Detail < blockers[j].Detail
	})
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func joinSorted(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	joined := ""
	for i, value := range sorted {
		if i > 0 {
			joined += ", "
		}
		joined += value
	}
	return joined
}
