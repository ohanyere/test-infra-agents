export type User = { id: string; email: string; full_name: string };
export type Organization = { id: string; name: string; slug: string };
export type CurrentWorkspace = { id: string; name: string; slug: string; organization: Organization; role: "owner" | "admin" | "member" | "viewer" };
export type AuthMeResponse = { user: User; workspaces: CurrentWorkspace[] };
export type WorkspaceMembership = { workspace_id?: string; user: User; role: "owner"|"admin"|"member"|"viewer"; joined_at: string };
export type SignupResponse = { user: User; organization: Organization; workspace: Pick<CurrentWorkspace,"id"|"name"|"slug"> };
export type APIErrorBody = { error: { code: string; message: string } };
export type AgentVersion = { id: string; version_label: string; interface_kind: string; tool_access_mode: "direct" | "mcp"; endpoint_url?: string; capabilities: string[]; claimed_capabilities: string[]; capability_catalog_version?: string; claim_status?: string; metadata: Record<string, unknown>; created_at: string };
export type Agent = { id: string; owner_workspace_id?: string; slug: string; name: string; description: string; visibility: string; status: string; latest_version: AgentVersion | null; created_at: string; updated_at: string };
export type AgentConnection = { agent_version_id: string; protocol: string; endpoint_url: string; auth_type: "none" | "bearer"; has_credential: boolean; credential_updated_at?: string; last_checked_at?: string; last_check_status: "untested" | "healthy" | "unreachable" | "authentication_failed" | "protocol_error" | "timeout" | "configuration_error"; last_diagnostic_code?: string };
export type AgentCapability = { id: string; kind: "observation" | "action"; label: string; category: string };
export type CompetencyCapability = { id: string; domain: string; family: string; display_name: string; description: string; catalog_version: string; status: "active" | "experimental" | "planned"; requires: string[] };
export type CapabilityCatalogResponse = { contract: string; items: CompetencyCapability[] };
export type CapabilityClaim = { capability_id: string; catalog_version: string; domain?: string; family?: string; display_name?: string; description?: string; claim_status: string; test_status: "untested" | "partial" | "tested" | string; qualification_status: null | string; claimed_at: string };
export type CapabilityClaimsResponse = { contract: string; claim_status: string; items: CapabilityClaim[]; qualification_note: string };
export type CoverageFamily = { family_id: string; required: boolean; valid_trials: number; distinct_variant_count: number; case_types_exercised: string[]; covered: boolean };
export type MissingRequirement = { code: string; family_id?: string; detail?: string };
export type CapabilityCoverage = { capability_id: string; claim_status: string; test_status: "untested" | "partial" | "tested" | string; coverage_contract_version: string; task_family_contract_version: string; capability_catalog_version: string; required_family_count: number; covered_family_count: number; valid_trial_count: number; distinct_variant_count: number; families: CoverageFamily[]; missing_requirements: MissingRequirement[]; suite_execution_id?: string; agent_version_id?: string };
export type CapabilityCoverageResponse = { coverage_contract_version: string; task_family_contract_version: string; capability_catalog_version: string; agent_version_id: string; suite_execution_id?: string; items: CapabilityCoverage[] };
export type TaskFamily = { id: string; domain: string; label: string; description: string; contract_version: string };
export type CertificationGate = { gate: string; required: boolean; status: "passed" | "failed" | "not_applicable" | string; code?: string; detail?: string };
export type CertificationRecord = { id: string; workspace_id: string; scenario_id: string; scenario_version_id: string; state: "certifying" | "certified" | "failed" | string; certification_contract_version: string; reference_remediation_contract_version: string; evaluator_version: string; runtime_contract_version: string; environment_profile: string; provider_id?: string; provider_contract_version?: string; isolation_tier?: string; environment_profile_contract_version?: string; environment_component_contract_version?: string; criteria_version: string; hidden_variant_contract_version: string; task_family_id?: string; case_type?: string; gate_results: CertificationGate[]; failure_codes: string[]; variant_ids: string[]; variant_seeds: number[]; started_at: string; completed_at?: string };
export type EnvironmentComponent = { id: string; display_name: string; description: string; contract_version: string; status: "active" | "experimental" | "planned" | string };
export type EnvironmentProfile = { id: string; display_name: string; description: string; contract_version: string; components: string[]; isolation_tier: "namespace" | "virtual_cluster" | "dedicated_cluster" | string; status: "active" | "experimental" | "planned" | string; priority_weight: number; runtime_contract_version: string };
export type EnvironmentProvider = { id: string; display_name: string; description: string; contract_version: string; status: "active" | "planned" | string; supported_profiles: string[]; production: boolean; development_only: boolean };
export type EnvironmentRequirements = { required_components: string[]; minimum_isolation_tier: string; required_profile_capabilities?: string[]; runtime_contract_version?: string; contract_version: string };
export type EnvironmentInstance = { id: string; workspace_id: string; owner_type: "run" | "certification" | string; owner_id: string; profile_id: string; profile_contract_version: string; provider_id: string; provider_contract_version: string; isolation_tier: string; status: "provisioning" | "ready" | "resetting" | "destroying" | "destroyed" | "failed" | string; lease_expires_at?: string; created_at: string; ready_at?: string; destroyed_at?: string; provider_ref: string };
export type CertificationStatus = { effective_state: "uncertified" | "certifying" | "certified" | "failed" | "stale" | string; eligible_authoritative_evidence: boolean; stale: boolean; stale_reasons: string[]; latest: CertificationRecord | null; history: CertificationRecord[]; certification_contract_version: string; evaluator_version: string };
export type QualificationBlocker = { code: string; capability_id?: string; family_id?: string; detail?: string; blocks_suite: boolean };
export type QualificationSelectedTask = { builtin_task_id: string; scenario_id: string; scenario_version_id: string; family_id: string; case_type: string; trial_count: number; sort_order: number; content_hash: string; environment_profile?: string; certification_id?: string; required_components?: string[]; minimum_isolation?: string };
export type QualificationCapabilityPlan = { capability_id: string; coverage_contract_version: string; required_families: string[]; min_distinct_families: number; covered_families: string[]; satisfied: boolean; selected_tasks: QualificationSelectedTask[]; eligible_candidate_count: number; blockers: QualificationBlocker[] };
export type QualificationPlan = { id: string; workspace_id: string; agent_id: string; agent_version_id: string; state: "ready" | "partial" | "blocked" | string; capability_ids: string[]; planner_contract_version: string; coverage_contract_version: string; task_family_contract_version: string; capability_catalog_version: string; certification_contract_version: string; evaluator_version: string; environment_profile_contract_version: string; capabilities: QualificationCapabilityPlan[]; blockers: QualificationBlocker[]; qualification_policy_available: boolean; generated_suite_version_id?: string; generated_suite_id?: string; created_by?: string; created_at: string };
export type QualificationSuiteBuild = { plan_id: string; suite_id: string; suite_version_id: string; entry_count: number; total_trials: number };
export type ReliabilityEmpirical = { total_trials: number; valid_trials: number; passed_trials: number; failed_trials: number; decisive_trials: number; inconclusive_trials: number; stopped_trials: number; pending_trials: number; platform_invalid_trials: number; pass_rate?: number | null; decisive_pass_rate?: number | null; valid_success_rate?: number | null; platform_invalid_rate?: number | null; safety_violation_trials: number };
export type ReliabilityKMetric = { k: number; available: boolean; pass_at_k?: number | null; pass_to_the_k?: number | null };
export type ReliabilityScopedSummary = { empirical: ReliabilityEmpirical; k_metrics: ReliabilityKMetric[]; sample_sufficiency: "insufficient" | "provisional" | "sufficient" | string; consistency_k3?: number | null };
export type ReliabilityDiversity = { distinct_variants: number; variant_trials: number; eligible_variants?: number; variant_coverage_ratio?: number; dominant_variant_share: number };
export type ReliabilityTaskPins = { scenario_version_id: string; agent_version_id: string; family_id?: string; case_type?: string; reliability_contract_version: string; reliability_policy_version: string; evaluator_contract_versions: string[]; runtime_contract_version: string; environment_refs?: string[]; environment_profiles?: string[]; provider_ids?: string[]; isolation_tiers?: string[]; distinct_suite_executions: number };
export type ReliabilityTask = { pins: ReliabilityTaskPins; empirical: ReliabilityEmpirical; k_metrics: ReliabilityKMetric[]; max_consecutive_passes: number; latest_consecutive_passes: number; variant_diversity: ReliabilityDiversity; sample_sufficiency: string; outcome_distribution: Record<string, number>; failure_breakdown: Record<string, number> };
export type ReliabilityFamily = { family_id: string; scenario_version_ids: string[]; distinct_scenario_versions: number; distinct_variants: number; valid_trials: number; passed_trials: number; failed_trials: number; decisive_trials: number; inconclusive_trials: number; stopped_trials: number; platform_invalid_trials: number; pass_rate?: number | null; decisive_pass_rate?: number | null; valid_success_rate?: number | null; consistency: ReliabilityKMetric[]; case_type_distribution: Record<string, number>; sample_sufficiency: string; breadth_note?: string; reliability_contract_version: string };
export type ReliabilityCapability = { capability_id: string; required_families: string[]; covered_families: string[]; families: ReliabilityFamily[]; valid_trial_count: number; summary: ReliabilityScopedSummary; contrast_trials: number; contrast_pass_rate?: number | null; reliability_state: "insufficient_evidence" | "limited_evidence" | "measured" | string; reliability_contract_version: string; coverage_contract_version: string; task_family_contract_version: string; requirement_available: boolean };
export type ReliabilitySaturation = { scenario_version_id: string; state: "fresh" | "developing" | "saturated" | string; reasons: string[]; executions: number; distinct_agent_versions: number; distinct_variants: number; outcome_distribution: Record<string, number>; recent_outcome_stability: string; family_alternatives: number; reliability_contract_version: string };
export type ReliabilityPolicy = { contract_version: string; min_valid_trials_per_scenario_version: number; target_valid_trials_per_scenario_version: number; min_valid_trials_per_family: number; target_valid_trials_per_family: number; min_distinct_variants: number; min_distinct_scenarios_per_family: number; min_contrast_trials: number };
export type AgentReliabilityReport = { agent_id: string; agent_version_id: string; reliability_contract_version: string; coverage_contract_version: string; reliability_policy: ReliabilityPolicy; summary: ReliabilityScopedSummary; tasks: ReliabilityTask[]; families: ReliabilityFamily[]; capabilities: ReliabilityCapability[]; executions: { suite_execution_id: string; summary: ReliabilityScopedSummary; distinct_scenarios: number; distinct_families: number; distinct_variants: number; platform_invalid_trials: number }[] };
export type ListResponse<T> = { items: T[]; pagination: { limit: number; offset: number } };
export type EvaluationBatch = { id: string; workspace_id: string; status: string; environment_mode: string; runs_per_agent: number; scenario: { scenario_id: string; scenario_version_id: string; name: string; version: number }; agents?: { agent_id: string; agent_version_id: string; name: string; version_label: string }[]; agent_count: number; run_count: number; created_at: string; started_at?: string; completed_at?: string };
export type Trial = { id: string; batch_id: string; agent: { agent_id: string; agent_version_id: string; name: string; version_label: string }; scenario?: { scenario_id: string; scenario_version_id: string; name: string; version: number }; attempt_number: number; status: string; environment_ref?: string; created_at: string; started_at?: string; completed_at?: string };
export type QualificationSuiteVersion = { id: string; suite_id: string; version: number; description: string; metadata: Record<string, unknown>; scenarios: { scenario_version_id: string; scenario_id?: string; scenario_name?: string; scenario_version?: number; trial_count: number; sort_order: number }[]; created_at: string };
export type QualificationSuite = { id: string; workspace_id: string; name: string; latest_version?: QualificationSuiteVersion; created_at: string };
export type SuiteExecution = { id: string; workspace_id: string; suite_version_id: string; status: string; agent_version_ids: string[]; batch_ids: string[]; created_at: string; started_at?: string; completed_at?: string };
export type SuiteResults = { execution_id: string; suite_version_id: string; overall: { total: number; completed: number; valid: number; invalid_platform: number; passed: number; failed: number; inconclusive: number; pending: number; running: number; pass_rate?: number | null; insufficient_samples: boolean }; trials: { run_id: string; agent_version_id: string; scenario_version_id: string; trial_ordinal: number; validity: string; evaluation_result?: string }[] };
export type EvaluationCriterion = { id: string; key: string; name: string; passed: boolean; observed_value?: unknown; evidence?: unknown };
export type TrialEvaluation = { id: string; run_id: string; result: string; evaluator_version: string; score?: number | null; summary: string; safety_violation_count: number; completed_at?: string; criteria?: EvaluationCriterion[]; criterion_results?: EvaluationCriterion[]; findings?: { kind: string; summary: string; evidence?: unknown }[] };
export type VerificationSnapshot = { contract_version: string; reference: string; captured_at: string; observation: { workloads: { kind: string; name: string; namespace: string; ready_replicas: number; desired_replicas: number; available_replicas: number; updated_replicas: number; image?: string; resource_version?: string }[]; pods: { name: string; namespace: string; phase: string; ready: boolean; restart_count: number; owning_workload?: string }[]; metrics: Record<string, unknown> } };
export type VerificationAction = { id: string; action_type: string; status: string; target_type?: string; target_ref?: string; authorized: boolean; requested: Record<string, unknown>; result: Record<string, unknown>; occurred_at: string };
export type VerificationChange = { resource: string; classification: "EXPECTED"|"UNEXPECTED"|"FORBIDDEN"|"UNATTRIBUTED"; attribution: "HIGH"|"UNKNOWN"; attribution_reason: string; fields: { field: string; before?: unknown; after?: unknown }[] };
export type VerificationFinding = { type: string; severity: string; summary: string; blocking: boolean; evidence: string[] };
export type ForwardRiskFinding = { analyzer_id: string; analyzer_version: string; type: string; severity: string; affected_resources: string[]; evidence: string[]; reason: string; trigger: string; evidence_strength: string; created_at: string };
export type VerificationResult = { id: string; workspace_id: string; run_id: string; agent_version_id: string; scenario_version_id: string; environment_instance_id?: string; composition_plan_id?: string; composition_plan_hash?: string; verifier_contract_version: string; snapshot_contract_version: string; baseline_snapshot: VerificationSnapshot; final_snapshot: VerificationSnapshot; intent: { objective: string; assertions: { key: string; metric: string; operator: string; target: unknown }[] }; action_inspection: { requested_count: number; authorized_count: number; denied_count: number; executed_count: number; failed_count: number; actions: VerificationAction[] }; infrastructure_diff: VerificationChange[]; outcome: { status: "PASSED"|"FAILED"|"INCONCLUSIVE"; reason: string; assertions: string[] }; side_effects: { status: string; findings: VerificationFinding[] }; stability: { status: "STABLE"|"UNSTABLE"|"INCONCLUSIVE"; reason: string; observations: number; started_at?: string; completed_at?: string }; forward_risks: ForwardRiskFinding[]; validity: "valid"|"invalid_platform"|"invalid_task"; overall_status: "VERIFIED"|"FAILED"|"WARNING"|"INCONCLUSIVE"; reason: string; evidence_started_at?: string; evidence_completed_at?: string; created_at: string };
export type QualificationTask = { id: string; name: string; description: string; domain: string; task_family: string; case_type: string; difficulty: string; duration: string; capabilities: string[]; required_components: string[]; minimum_isolation_tier: string; runtime_contract_version: string; runnable: boolean; eligible_profile?: string; scheduler_reason: string; certification_note: string };

export const listAgents = (workspaceID: string) => apiRequest<ListResponse<Agent>>(`/v1/workspaces/${workspaceID}/agents?status=all`);
export const listBatches = (workspaceID: string, limit = 50) => apiRequest<ListResponse<EvaluationBatch>>(`/v1/workspaces/${workspaceID}/evaluation-batches?limit=${limit}&offset=0`);
export const listBatchTrials = (workspaceID: string, batchID: string) => apiRequest<ListResponse<Trial>>(`/v1/workspaces/${workspaceID}/evaluation-batches/${batchID}/runs?limit=100&offset=0`);
export const getTrial = (workspaceID: string, runID: string) => apiRequest<Trial>(`/v1/workspaces/${workspaceID}/runs/${runID}`);
export const getTrialEvaluation = (workspaceID: string, runID: string) => apiRequest<TrialEvaluation>(`/v1/workspaces/${workspaceID}/runs/${runID}/evaluation`);
export const getVerification = (workspaceID: string, runID: string) => apiRequest<VerificationResult>(`/v1/workspaces/${workspaceID}/runs/${runID}/verification`);
export const listSuites = (workspaceID: string) => apiRequest<ListResponse<QualificationSuite>>(`/v1/workspaces/${workspaceID}/evaluation-suites?limit=50&offset=0`);
export const getSuite = (workspaceID: string, suiteID: string) => apiRequest<QualificationSuite>(`/v1/workspaces/${workspaceID}/evaluation-suites/${suiteID}`);
export const startSuite = (workspaceID: string, suiteVersionID: string, agentVersionIDs: string[]) => apiRequest<SuiteExecution>(`/v1/workspaces/${workspaceID}/suite-executions`, { method: "POST", body: JSON.stringify({ suite_version_id: suiteVersionID, agent_version_ids: agentVersionIDs }) });
export const getSuiteExecution = (workspaceID: string, executionID: string) => apiRequest<SuiteExecution>(`/v1/workspaces/${workspaceID}/suite-executions/${executionID}`);
export const getSuiteResults = (workspaceID: string, executionID: string) => apiRequest<SuiteResults>(`/v1/workspaces/${workspaceID}/suite-executions/${executionID}/results`);
export const listQualificationTasks = () => apiRequest<{ items: QualificationTask[] }>("/v1/qualification-tasks");
export type TrajectoryEvent = { event_id: string; sequence: number; occurred_at: string; relative_ms?: number | null; event_type: string; phase: string; actor: string; summary: string; status: string; source_type: string; source_id: string; evidence_refs: string[]; safe_metadata: Record<string, unknown>; view: "agent_visible" | "evaluator" | string };
export type TrajectorySummary = { trial_status: string; evaluation_status: string; validity: string; pass_fail_outcome: string; failure_classes: string[]; finding_count: number; safety_violation_count: number; actions_requested: number; actions_executed: number; actions_denied: number; observations_count: number; time_to_recovery_ms?: number | null; recovery_verified: boolean; actions_after_recovery: number; actions_before_recovery: number };
export type TrajectoryDurations = { trial_duration_ms?: number | null; environment_setup_duration_ms?: number | null; agent_active_duration_ms?: number | null; time_to_first_action_ms?: number | null; time_to_recovery_ms?: number | null; verification_duration_ms?: number | null };
export type TrajectoryPins = { agent_version_id: string; scenario_version_id: string; suite_execution_id?: string | null; suite_version_id?: string | null; hidden_variant_id?: string | null; runtime_contract: string; evaluator_contract: string; trajectory_contract: string; environment_profile_id?: string | null; environment_provider_id?: string | null; environment_isolation_tier?: string | null; trial_ordinal: number };
export type Trajectory = { contract: string; run_id: string; workspace_id: string; ordering_rule: string; events: TrajectoryEvent[]; summary: TrajectorySummary; durations: TrajectoryDurations; pins: TrajectoryPins; recovery_detected_at?: string | null; recovery_verified_at?: string | null; total_events: number; truncated: boolean };
export type EvidenceDetail = { evidence_id: string; source_type: string; source_id: string; occurred_at?: string | null; summary: string; view: string; safe_detail: Record<string, unknown>; preview?: string; truncated: boolean; byte_size: number; content_hash: string; provenance: string[] };
export type SuiteTrajectoryTrial = { run_id: string; trial_ordinal: number; agent_version_id: string; scenario_version_id: string; outcome: string; validity: string; actions_requested: number; actions_executed: number; actions_denied: number; safety_violations: number; time_to_recovery_ms?: number | null; recovery_verified: boolean };
export type SuiteTrajectoryIssue = { code: string; run_id?: string | null; detail?: string };
export type SuiteTrajectorySummary = { contract: string; suite_execution_id: string; workspace_id: string; trials: SuiteTrajectoryTrial[]; total_trials: number; complete: boolean; expected_trials: number; loaded_trials: number; missing_trials?: string[] | null; issues?: SuiteTrajectoryIssue[] | null };
export type TrajectoryComparison = { contract: string; baseline_run_id: string; candidate_run_id: string; baseline_outcome: string; candidate_outcome: string; baseline_actions: number; candidate_actions: number; baseline_denied: number; candidate_denied: number; baseline_recovery_ms?: number | null; candidate_recovery_ms?: number | null; baseline_verified: boolean; candidate_verified: boolean; baseline_findings: number; candidate_findings: number; baseline_failure_classes: string[]; candidate_failure_classes: string[] };

export class APIError extends Error {
  constructor(public readonly status: number, public readonly code: string, message: string) { super(message); this.name = "APIError"; }
}

export type BillingAccount = { workspace_id: string; plan_id: string; status: string; period_start: string; period_end: string; catalog_version: string; provider?: string };
export type BillingPlan = { id: string; display_name: string; price_cents: number; price_display: string; cadence: string; monthly_credits: number; max_members: number; max_parallel_trials: number; evidence_retention_days: number; max_isolation: string };
export type EntitlementDecision = { key: string; allowed: boolean; limit?: number; limit_label?: string; value?: string; reason: string; code?: string };
export type UsageEntry = { id: string; kind: string; quantity: number; reason?: string; run_id?: string; created_at: string };
export type UsageSummary = { period_start: string; period_end: string; allowance: number; allowance_label: string; granted: number; reserved: number; committed: number; available: number; unlimited: boolean; meter_contract: string; recent_entries: UsageEntry[] };
export type BillingResponse = { account: BillingAccount; catalog_contract: string; plans: BillingPlan[]; provider_backed: boolean };
export type EntitlementsResponse = { account: BillingAccount; contract: string; items: EntitlementDecision[] };
export type UsageResponse = { account: BillingAccount; usage: UsageSummary };

export function getBilling(workspaceID: string) { return apiRequest<BillingResponse>(`/v1/workspaces/${workspaceID}/billing`); }
export function getEntitlements(workspaceID: string) { return apiRequest<EntitlementsResponse>(`/v1/workspaces/${workspaceID}/entitlements`); }
export function getUsage(workspaceID: string) { return apiRequest<UsageResponse>(`/v1/workspaces/${workspaceID}/usage`); }

const configuredBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL?.replace(/\/$/, "");
export const API_BASE_URL = configuredBaseURL || (process.env.NODE_ENV === "production" ? "" : "http://localhost:8080");

function apiBaseURL(): string {
  if (!API_BASE_URL) {
    throw new APIError(0, "configuration_error", "The API service has not been configured.");
  }
  return API_BASE_URL;
}

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const baseURL = apiBaseURL();
  let response: Response;
  try { response = await fetch(new URL(path, `${baseURL}/`), { ...init, headers, credentials: "include" }); }
  catch { throw new APIError(0, "network_error", "We couldn't reach the service. Try again."); }
  if (!response.ok) {
    let payload: APIErrorBody | undefined;
    try { payload = await response.json() as APIErrorBody; } catch { /* use safe fallback */ }
    throw new APIError(response.status, payload?.error?.code || "request_failed", payload?.error?.message || "The service couldn't complete the request.");
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export type TrajectoryFilters = { limit?: number; offset?: number; phase?: string; event_type?: string; actor?: string; status?: string };

export function getTrajectory(workspaceID: string, runID: string, filters: TrajectoryFilters = {}): Promise<Trajectory> {
  const params = new URLSearchParams();
  if (filters.limit !== undefined) params.set("limit", String(filters.limit));
  if (filters.offset !== undefined) params.set("offset", String(filters.offset));
  if (filters.phase) params.set("phase", filters.phase);
  if (filters.event_type) params.set("event_type", filters.event_type);
  if (filters.actor) params.set("actor", filters.actor);
  if (filters.status) params.set("status", filters.status);
  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  return apiRequest<Trajectory>(`/v1/workspaces/${workspaceID}/runs/${runID}/trajectory${suffix}`);
}

export function getTrajectoryEvidence(workspaceID: string, runID: string, evidenceID: string): Promise<EvidenceDetail> {
  return apiRequest<EvidenceDetail>(`/v1/workspaces/${workspaceID}/runs/${runID}/evidence/${encodeURIComponent(evidenceID)}`);
}

export function getSuiteTrajectorySummary(workspaceID: string, executionID: string): Promise<SuiteTrajectorySummary> {
  return apiRequest<SuiteTrajectorySummary>(`/v1/workspaces/${workspaceID}/suite-executions/${executionID}/trajectory-summary`);
}

export function compareTrajectories(workspaceID: string, baselineRunID: string, candidateRunID: string): Promise<TrajectoryComparison> {
  return apiRequest<TrajectoryComparison>(`/v1/workspaces/${workspaceID}/runs/${baselineRunID}/trajectory?compare_with=${encodeURIComponent(candidateRunID)}`);
}
