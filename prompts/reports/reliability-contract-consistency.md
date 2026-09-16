# Reliability Contract Consistency — Completion Report (Stage 33 patch)

Source task: `prompts/tasks/reliability-contract-consistency.md` (read completely; this report does not overwrite it). Narrow correctness patch only; no reliability-subsystem redesign, no LLM, no Docker/Kubernetes/Argo CD.

## 1. Root causes

1. **Coverage contract unpinned.** `reliability/capability.go` called `coverage.GetRequirement` (historical V1 API) and hardcoded `CoverageContract: coverage.CoverageContractVersion` (v1) on every capability, while the Stage-32 qualification planner builds against `capability-coverage-contract:v2`. Reliability therefore silently used V1/global semantics: GitOps/delivery capabilities were always `requirement_available=false`, and no report recorded which coverage view was used.
2. **Environment identity was the physical ref.** `reliability/comparability.go` required `sameStrings(base.EnvironmentRefs, candidate.EnvironmentRefs)`. Two samples on different instance IDs of the same profile/provider/isolation were `environment_fidelity_mismatch`, while two opaque refs with no semantic metadata could compare equal when both were empty. `Trial` carried only `EnvironmentRef`; the `environment_instances` semantic pins (profile, provider, isolation) were never selected.
3. **Pass-rate denominator ambiguous.** `Empirical.PassRate = passed/(passed+failed)` omitted valid inconclusive/stopped trials, while `KMetrics` used `n = all valid trials`. A sample with inconclusive evidence showed a decisive pass rate alongside k-metrics computed over a larger denominator, with no `decisive_trials` / `valid_success_rate` fields documenting the difference.

## 2. Coverage contract behavior

- `coverage` V1 (`requirements.go`: 8 requirements, `capability-coverage-contract:v1`) is byte-identical and untouched. `GetRequirement`/`ListRequirements` still serve V1 only.
- `coverage` V2 (`requirements_v2.go`: `capability-coverage-contract:v2`) still serves the 10 GitOps/delivery additions plus unchanged V1 entries via `GetRequirementV2`/`ListRequirementsV2`.
- Reliability no longer silently uses "current/global" semantics: the selected contract is an explicit constant plus an explicit lookup function, pinned on every report and every capability.

## 3. Current coverage contract used by reliability

- `reliability/capability.go`: `CurrentCoverageContractVersion = coverage.CoverageContractV2` (the current qualification coverage contract, matching `qualplan.CoverageContractVersion`).
- `BuildCapabilityReliabilities` (current) aggregates under the V2 view (`coverage.GetRequirementV2`); per-capability `CoverageContract` carries the exact requirement contract (V1 entries stay `v1` inside the V2 view; V2-only entries carry `v2`).
- `BuildCapabilityReliabilitiesV1` (new, frozen) aggregates under `coverage.GetRequirement` for historical reproducibility.
- Report-level pins added: `AgentReport.CoverageContract`, `ExecutionReport.CoverageContract`, `ExecutionAgentEntry.CoverageContract`, `ScenarioReport.CoverageContract` — all set by `reliability/service.go` to `CurrentCoverageContractVersion`.
- Unavailable capabilities under the selected contract return the structured state (`RequirementAvailable=false`, `ReliabilityState=insufficient_evidence`, empty families, zero trials, empty summary) while still pinning the selected contract — never fabricated reliability.

## 4. GitOps/delivery reliability behavior

- GitOps/delivery claims now resolve through the V2 view: `gitops.drift.detection`, `gitops.argocd.recovery`, `delivery.rollout.recovery`, and the other 7 V2 capabilities report `requirement_available=true` with their two-family requirements and `coverage_contract_version=v2`.
- V1 builder still reports those same IDs as unavailable (regression test), proving V1 history is unchanged.
- Coverage breadth truth still comes from the requirement (min 2 families / 2 trials / contrast groups); reliability never redefines it.

## 5. Environment semantic comparability

- `Trial` gains semantic pins: `EnvironmentProfileID`, `EnvironmentProfileContract`, `ProviderID`, `ProviderContract`, `IsolationTier` (populated from the run-owned `environment_instances` row, newest first, via correlated sub-selects — no row duplication).
- `Pins` gains distinct sorted sets: `EnvironmentProfiles`, `EnvironmentProfileContracts`, `ProviderIDs`, `ProviderContracts`, `IsolationTiers`; `EnvironmentRefs` remains provenance only.
- `CheckComparability` compares semantic sets, never raw refs: different instance refs with identical profile/profile-contract/provider/provider-contract/isolation are comparable; any profile, profile-contract, provider-ID/class, provider-contract, or isolation-tier mismatch yields `environment_fidelity_mismatch`. Namespace vs dedicated (and virtual-cluster) distinctions are exact-set comparisons, never weakened. Runtime/evaluator/scope/distribution rules are unchanged.
- Missing semantic metadata on either side yields the new structured limitation `environment_semantic_unavailable` (added in `contract.go`) instead of inferring fidelity from opaque refs.

## 6. Environment provenance behavior

- Exact `environment_ref` values are retained: `Pins.EnvironmentRefs` and `TaskPins.EnvironmentRefs` carry the distinct sorted opaque refs; `TaskPins` additionally carries `EnvironmentProfiles`, `ProviderIDs`, `IsolationTiers` as semantic provenance.
- Comparability never uses refs as identity; provenance fields are read-only metadata (no credentials — refs are the existing safe opaque handles).

## 7. Pass-rate denominator decision

Design **B** (retain decisive-only rate, expose both denominators explicitly):

- `Empirical.PassRate` (historical V1, frozen) = `passed / (passed + failed)`, nil when decisive == 0.
- `Empirical.DecisivePassRate` (new, explicit alias) = same decisive value, nil when decisive == 0.
- `Empirical.DecisiveTrials` (new) = `passed + failed`.
- `Empirical.ValidSuccessRate` (new) = `passed / valid_trials`, nil when valid == 0; every valid non-pass (fail, inconclusive, stopped, unknown) is non-success here.
- `FamilyReliability` exposes the same split: `decisive_trials`, `inconclusive_trials`, `stopped_trials`, `decisive_pass_rate`, `valid_success_rate` alongside frozen `pass_rate`.
- K-metrics denominator documented on `KMetrics`: `n = all valid trials` (inconclusive/stopped are non-success), `c = passes`.

## 8. Inconclusive/stopped behavior

- Valid inconclusive/stopped trials count in `ValidTrials`, are exposed as `InconclusiveTrials`/`StoppedTrials` at empirical and family scope, are excluded from the decisive denominator, and count as non-success in `ValidSuccessRate` and in k-estimation (`n` includes them, `c` counts passes only). Tested (`TestConsistencyInconclusiveBehavior` vector: 1 pass + 1 inconclusive + 1 stopped): `valid_trials=3`, `decisive_trials=1`, decisive pass rate 100%, valid-success 33.3%, k=1 estimates 1/3 (n=3/c=1).
- `FailureBreakdown`/`OutcomeDistribution` behavior unchanged; platform-invalid stays outside all agent denominators.

## 9. Contract-version decision

- `reliability-metrics-contract:v1` is **retained, not bumped**: `PassRate` semantics are frozen and `PassRate == DecisivePassRate` always; the fix is purely additive (`decisive_trials`, `decisive_pass_rate`, `valid_success_rate`, family inconclusive/stopped counts). No silent V1 mutation.
- Coverage versioning is explicit selection, not mutation: V1 map untouched; reliability pins `CurrentCoverageContractVersion=v2` at report level and the exact requirement contract per capability; `BuildCapabilityReliabilitiesV1` preserves the historical view.
- Comparability adds one reason code (`environment_semantic_unavailable`) alongside the existing `environment_fidelity_mismatch`; no existing code is renamed or weakened.

## 10. Files changed

Backend (`backend/internal/reliability/`):

- `metrics.go` — Design-B fields (`DecisiveTrials`, `DecisivePassRate`, `ValidSuccessRate`) + denominator contract docs + computation.
- `capability.go` — `CoverageContractV1/V2`, `CurrentCoverageContractVersion`, `BuildCapabilityReliabilities` (V2 view) + `BuildCapabilityReliabilitiesV1` (frozen V1) + per-requirement contract pin + structured unavailable.
- `contract.go` — `NotComparableEnvironmentUnknown = "environment_semantic_unavailable"`.
- `trial.go` — semantic environment pins (provenance note for `EnvironmentRef`).
- `comparability.go` — semantic `Pins` + `HasSemanticEnvironment` + semantic comparison + unknown-metadata limitation.
- `task.go` — `TaskPins` semantic provenance (`EnvironmentProfiles`, `ProviderIDs`, `IsolationTiers`).
- `family.go` — decisive/inconclusive/stopped + explicit rates.
- `report.go` — `CoverageContract` on `AgentReport`, `ExecutionReport`, `ExecutionAgentEntry`, `ScenarioReport`.
- `service.go` — sets `CoverageContract: CurrentCoverageContractVersion` on all reports/entries.
- `repository.go` — correlated sub-selects for the 5 semantic pins from `environment_instances` (no JOIN duplication); `scanTrials` extended.
- `reliability_test.go` — `trial()` helper now carries semantic defaults (profile `kubernetes.namespace:v1`, provider `kind-dev:v1`, isolation `namespace`, runtime default, ref `env-instance-1`) so existing comparability tests exercise the new rules.
- `contract_consistency_test.go` (new) — 14 focused tests (see §11).

Frontend (`lib/api.ts`, additive only):

- `ReliabilityEmpirical` += `decisive_trials`, `decisive_pass_rate?`, `valid_success_rate?`.
- `ReliabilityFamily` += `decisive_trials`, `inconclusive_trials`, `stopped_trials`, `decisive_pass_rate?`, `valid_success_rate?`.
- `ReliabilityTaskPins` += `environment_profiles?`, `provider_ids?`, `isolation_tiers?`.
- `AgentReliabilityReport` += `coverage_contract_version`.

No migrations, no evaluator/execution/planner/qualification changes, no API renames (additive fields only; existing consumers unaffected).

## 11. Focused tests

New `backend/internal/reliability/contract_consistency_test.go` (14 tests, one per task requirement):

1. `TestConsistencyReportPinsCoverageContract` — current contract is V2; V1 caps pin v1, V2 caps pin v2.
2. `TestConsistencyV1SemanticsUnchanged` — V1 stays 8 requirements with contrast group; V1 builder pins v1 and reports GitOps unavailable.
3. `TestConsistencyV2GitOpsVisible` — GitOps/delivery requirements visible with breadth under current contract.
4. `TestConsistencyRequirementUnavailableStructured` — unknown capability: unavailable, insufficient, no fabricated families/trials/summary, selected contract pinned.
5. `TestConsistencySemanticComparableAcrossRefs` — same semantics, different refs → comparable.
6. `TestConsistencyIsolationMismatchNotComparable` — namespace vs dedicated → `environment_fidelity_mismatch`.
7. `TestConsistencyContractMismatchNotComparable` — provider-contract and profile mismatch → not comparable.
8. `TestConsistencyEnvRefProvenance` — exact refs retained in `Pins` and `TaskPins`.
9. `TestConsistencyPassRateDenominator` — decisive 50 vs valid-success 25 with `decisive_trials=2`.
10. `TestConsistencyInconclusiveBehavior` — inconclusive/stopped exposed; k=1 uses n=3/c=1.
11. `TestConsistencyPlatformInvalidExcluded` — platform outside decisive/valid denominators.
12. `TestConsistencyKVectorUnchanged` — n=5/c=3/k=2 → 0.9/0.3.
13. `TestConsistencyNoHistoricalMutation` — reliability v1 + coverage v1 pins frozen; decisive rate unchanged.
14. `TestConsistencyNoLLMDeterministic` — builders byte-identical on repeat inputs (no LLM/randomness).

Focused runs (per task scope; `go test ./...` not run; no Docker/K8s):

- `go test ./internal/reliability/... ./internal/coverage/...` — ok.
- `go test ./internal/environment/... ./internal/qualplan/...` — ok.
- `go test ./internal/httpapi/ -run 'TestReliability|TestQualification' -v` — all PASS (6 reliability + 9 qualification).
- `go vet` on reliability/coverage/environment/qualplan — clean; `gofmt -l` — clean.

## 12. TypeScript/build if applicable

- `npx tsc --noEmit` — clean (exit 0).
- `npm run build` — production build succeeds (exit 0); required because frontend types changed (additive optional fields only, no UI logic change).

## 13. Confirmation pass@k/pass^k unchanged

Confirmed: estimators in `metrics.go` untouched (`1 - prod(n-c-i)/(n-i)`, `prod(c-i)/(n-i)`); spec vectors re-pinned in `TestConsistencyKVectorUnchanged` (n=5/c=3/k=2 → pass@2=0.9, pass^2=0.3) and all pre-existing math tests (`TestMathVectorN5C3K2`, `TestMathVectorAllPass/Fail`, `TestMixedPassAtKExceedsPassToTheK`, `TestPassAtKAndPassToTheKDiffer`) still pass. K-metrics still use n=all valid trials; only documentation + surrounding denominator fields were added.

## 14. Confirmation platform-invalid remains excluded

Confirmed: `Summarize` validity predicate unchanged (only `valid` enters denominators); `TestConsistencyPlatformInvalidExcluded` plus pre-existing `TestMixedDenominatorAndPlatformExclusion`/`TestPlatformInvalidRateSeparate` assert a valid pass + platform-invalid pair yields 100% agent/decisive/valid-success rates with platform counted separately (`platform_invalid_trials`, `platform_invalid_rate`). `FamilyReliability` and capability summaries inherit the same predicate.

## 15. Confirmation no historical coverage/reliability contract silently mutated

Confirmed:

- `reliability-metrics-contract:v1` frozen (`TestContractPinned`, `TestConsistencyNoHistoricalMutation`); `PassRate` still decisive-only and equals `DecisivePassRate`.
- `capability-coverage-contract:v1` frozen at 8 requirements (`TestV1RequirementsUnchangedByV2` in coverage plus `TestConsistencyV1SemanticsUnchanged` here); V1 entries surfaced through the V2 view keep their v1 contract (`TestV2PreservesV1Entries` still passes).
- `BuildCapabilityReliabilitiesV1` reproduces the old `GetRequirement` behavior exactly; the current-path switch to the V2 view is explicit, pinned, and tested — not silent.

## 16. Correction — §8 decisive-rate wording (prompts/reports/reliability-contracts-consistency.md)

- Root cause: completion-report wording mistake only. §8 conflated the `TestConsistencyPassRateDenominator` vector (1 pass + 1 fail + 1 inconclusive + 1 stopped → decisive 50%) with the `TestConsistencyInconclusiveBehavior` vector (1 pass + 1 inconclusive + 1 stopped). `Summarize` (`backend/internal/reliability/metrics.go`: `DecisiveTrials = passed + failed`, `PassRate`/`DecisivePassRate = passed / decisive`, `ValidSuccessRate = passed / valid`) and `KMetrics` (n = all valid, c = passes) were already correct, and the focused tests asserted the correct denominators (decisive=2/valid-success 25 for the 4-trial vector; n=3/c=1 k=1 for the 3-trial vector).
- Exact input vector (`TestConsistencyInconclusiveBehavior`): 1 valid pass + 1 valid inconclusive + 1 valid stopped.
- Exact result: `valid_trials=3`, `decisive_trials=1` (`passed=1`, `failed=0`), decisive pass rate 100%, valid-success rate 33.3% (100/3), k=1 `pass@k = pass^k = 1/3` (n=3/c=1). A 50% decisive pass rate requires one pass + one fail (the §11 item 9 vector).
- Production code changed: no. Only §8 wording above was corrected to match the test vector.
- Focused test result: `go test ./internal/reliability/... -count=1` — ok.
