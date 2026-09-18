"use client";

import { useCallback, useEffect, useState } from "react";
import { APIError, apiRequest, type Agent, type AgentConnection, type AgentReliabilityReport, type AgentVersion, type CapabilityCatalogResponse, type CapabilityClaimsResponse, type CapabilityCoverageResponse, type CompetencyCapability, type ListResponse, type QualificationPlan, type QualificationSuiteBuild, type ReliabilityKMetric } from "../../../lib/api";
import { useAuth } from "../../../lib/auth";
import styles from "./agent-detail.module.css";

const statusText: Record<string, string> = { untested: "Untested", healthy: "Healthy", unreachable: "Unreachable", authentication_failed: "Authentication failed", protocol_error: "Protocol error", timeout: "Timed out", configuration_error: "Configuration error" };
const testStatusText: Record<string, string> = { untested: "UNTESTED", partial: "PARTIAL", tested: "TESTED" };
const planStateText: Record<string, string> = { ready: "READY", partial: "PARTIAL", blocked: "BLOCKED" };
const sufficiencyText: Record<string, string> = { insufficient: "Insufficient", provisional: "Provisional", sufficient: "Sufficient" };
const reliabilityStateText: Record<string, string> = { insufficient_evidence: "Insufficient evidence", limited_evidence: "Limited evidence", measured: "Measured" };

function formatPct(value?: number | null) {
  if (value === undefined || value === null) return "—";
  return (Math.round(value * 10) / 10).toFixed(1) + "%";
}

function kEstimate(metrics: ReliabilityKMetric[], k: number, key: "pass_at_k" | "pass_to_the_k") {
  const metric = metrics.find((item) => item.k === k);
  if (!metric || !metric.available || metric[key] === undefined || metric[key] === null) return "insufficient valid trials";
  return formatPct(metric[key]);
}

export function AgentDetail({ agentID }: { agentID: string }) {
  const { workspace, loading: authLoading, error: authError, retry } = useAuth();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [versions, setVersions] = useState<AgentVersion[]>([]);
  const [connections, setConnections] = useState<Record<string, AgentConnection>>({});
  const [claims, setClaims] = useState<Record<string, CapabilityClaimsResponse>>({});
  const [coverage, setCoverage] = useState<Record<string, CapabilityCoverageResponse>>({});
  const [reliability, setReliability] = useState<Record<string, AgentReliabilityReport>>({});
  const [catalog, setCatalog] = useState<CompetencyCapability[]>([]);
  const [plans, setPlans] = useState<Record<string, QualificationPlan>>({});
  const [planBusy, setPlanBusy] = useState("");
  const [planError, setPlanError] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [rotation, setRotation] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    if (!workspace) return;
    setError("");
    try {
      const agentValue = await apiRequest<Agent>("/v1/workspaces/" + workspace.id + "/agents/" + agentID);
      const versionValues = await apiRequest<ListResponse<AgentVersion>>("/v1/workspaces/" + workspace.id + "/agents/" + agentID + "/versions");
      setAgent(agentValue); setVersions(versionValues.items);
      try {
        const catalogValue = await apiRequest<CapabilityCatalogResponse>("/v1/capability-catalog");
        setCatalog(catalogValue.items);
      } catch { /* claims still render from version payload */ }
      const statuses = await Promise.all(versionValues.items.map(async (version) => {
        try { return [version.id, await apiRequest<AgentConnection>("/v1/workspaces/" + workspace.id + "/agent-versions/" + version.id + "/connection")] as const; }
        catch { return [version.id, null] as const; }
      }));
      const safe: Record<string, AgentConnection> = {};
      statuses.forEach(([id, value]) => { if (value) safe[id] = value; });
      setConnections(safe);
      const claimValues = await Promise.all(versionValues.items.map(async (version) => {
        try { return [version.id, await apiRequest<CapabilityClaimsResponse>("/v1/workspaces/" + workspace.id + "/agents/" + agentID + "/versions/" + version.id + "/capability-claims")] as const; }
        catch { return [version.id, null] as const; }
      }));
      const claimSafe: Record<string, CapabilityClaimsResponse> = {};
      claimValues.forEach(([id, value]) => { if (value) claimSafe[id] = value; });
      setClaims(claimSafe);
      const coverageValues = await Promise.all(versionValues.items.map(async (version) => {
        try { return [version.id, await apiRequest<CapabilityCoverageResponse>("/v1/workspaces/" + workspace.id + "/agents/" + agentID + "/versions/" + version.id + "/capability-coverage")] as const; }
        catch { return [version.id, null] as const; }
      }));
      const coverageSafe: Record<string, CapabilityCoverageResponse> = {};
      coverageValues.forEach(([id, value]) => { if (value) coverageSafe[id] = value; });
      setCoverage(coverageSafe);
      const reliabilityValues = await Promise.all(versionValues.items.map(async (version) => {
        try { return [version.id, await apiRequest<AgentReliabilityReport>("/v1/workspaces/" + workspace.id + "/agents/" + agentID + "/versions/" + version.id + "/reliability")] as const; }
        catch { return [version.id, null] as const; }
      }));
      const reliabilitySafe: Record<string, AgentReliabilityReport> = {};
      reliabilityValues.forEach(([id, value]) => { if (value) reliabilitySafe[id] = value; });
      setReliability(reliabilitySafe);
    } catch (reason) { setError(reason instanceof APIError ? reason.message : "Agent details could not be loaded."); }
  }, [workspace, agentID]);

  useEffect(() => { void load(); }, [load]);

  async function testConnection(versionID: string) {
    if (!workspace) return; setBusy("test-" + versionID); setError("");
    try {
      const value = await apiRequest<AgentConnection>("/v1/workspaces/" + workspace.id + "/agent-versions/" + versionID + "/connection/test", { method: "POST", body: "{}" });
      setConnections((current) => ({ ...current, [versionID]: value }));
    } catch (reason) { setError(reason instanceof APIError ? reason.message : "Connection could not be tested."); }
    finally { setBusy(""); }
  }

  async function rotate(versionID: string) {
    if (!workspace || !rotation[versionID]) return; setBusy("rotate-" + versionID); setError("");
    try {
	  const value = await apiRequest<AgentConnection>("/v1/workspaces/" + workspace.id + "/agent-versions/" + versionID + "/credential", { method: "PUT", body: JSON.stringify({ auth_type: "bearer", bearer_token: rotation[versionID] }) });
      setConnections((current) => ({ ...current, [versionID]: value })); setRotation((current) => ({ ...current, [versionID]: "" }));
	} catch (reason) { setError(reason instanceof APIError ? reason.message : "Credential could not be updated."); }
    finally { setBusy(""); }
  }

  function claimsFor(version: AgentVersion) {
    const response = claims[version.id];
    if (response) return response.items;
    const ids = version.claimed_capabilities || [];
    return ids.map((id) => {
      const meta = catalog.find((item) => item.id === id);
      return { capability_id: id, catalog_version: version.capability_catalog_version || "infrastructure-capability-catalog:v1", domain: meta?.domain || "", family: meta?.family || "", display_name: meta?.display_name || id, description: meta?.description || "", claim_status: "claimed", test_status: "untested", qualification_status: null, claimed_at: version.created_at };
    });
  }

  function coverageFor(versionID: string, capabilityID: string) {
    return coverage[versionID]?.items.find((item) => item.capability_id === capabilityID);
  }

  async function buildPlan(version: AgentVersion) {
    if (!workspace) return;
    setPlanBusy("plan-" + version.id);
    setPlanError((current) => ({ ...current, [version.id]: "" }));
    try {
      const plan = await apiRequest<QualificationPlan>("/v1/workspaces/" + workspace.id + "/agents/" + agent?.id + "/versions/" + version.id + "/qualification-plans", { method: "POST", body: "{}" });
      setPlans((current) => ({ ...current, [version.id]: plan }));
    } catch (reason) {
      setPlanError((current) => ({ ...current, [version.id]: reason instanceof APIError ? reason.message : "Qualification plan could not be built." }));
    } finally {
      setPlanBusy("");
    }
  }

  async function buildSuite(versionID: string) {
    if (!workspace) return;
    const plan = plans[versionID];
    if (!plan) return;
    setPlanBusy("suite-" + versionID);
    setPlanError((current) => ({ ...current, [versionID]: "" }));
    try {
      const result = await apiRequest<{ build: QualificationSuiteBuild }>("/v1/workspaces/" + workspace.id + "/qualification-plans/" + plan.id + "/suite", { method: "POST", body: "{}" });
      const refreshed = await apiRequest<QualificationPlan>("/v1/workspaces/" + workspace.id + "/qualification-plans/" + plan.id);
      setPlans((current) => ({ ...current, [versionID]: { ...refreshed, generated_suite_version_id: result.build.suite_version_id, generated_suite_id: result.build.suite_id } }));
    } catch (reason) {
      setPlanError((current) => ({ ...current, [versionID]: reason instanceof APIError ? reason.message : "Qualification suite could not be created." }));
    } finally {
      setPlanBusy("");
    }
  }

  if (authLoading || (!agent && !error)) return <main className={styles.page}><p className={styles.state}>Loading agent…</p></main>;
  if (authError || error || !agent) return <main className={styles.page}><div className={styles.state}><p>{authError || error || "Agent not found."}</p><button onClick={authError ? retry : load}>Try again</button></div></main>;

  return <main className={styles.page}>
    <header className={styles.topNav}><a href="/">evalplane</a><a href="/agents">Agents</a></header>
    <header className={styles.agentHeader + " " + styles.shell}><p className={styles.label}>AGENT</p><h1>{agent.name}</h1><p className={styles.description}>{agent.description || "No description provided."}</p><div className={styles.actions}><a className={styles.primaryButton} href={"/agents/new?agent=" + agent.id}>Register version</a><a className={styles.secondaryButton} href="/verifications">Verifications</a></div><nav aria-label="Agent sections"><a href="#versions">Versions</a> · <a href="/verifications">Verifications</a> · <a href="#versions">Connection</a> · <a href="/integrations">CI</a></nav></header>
	<section id="versions" className={styles.section + " " + styles.shell}><div className={styles.sectionIntro}><p className={styles.label}>VERSIONS · CONNECTION · VERIFICATION</p><h2>Exact versions and their evidence.</h2><p>Behavior changes create a new version. Connection credentials can rotate without changing version identity. Verification outcomes remain attached to the exact version that acted.</p></div>
      <div className={styles.versionList}>{versions.map((version) => { const connection = connections[version.id]; const versionClaims = claimsFor(version); return <article className={styles.versionRow} key={version.id}>
        <div><h3>{version.version_label}</h3><code>{version.id}</code></div><dl><div><dt>Invocation</dt><dd>{version.interface_kind.toUpperCase()}</dd></div><div><dt>Tool access</dt><dd>{version.tool_access_mode === "mcp" ? "MCP" : "Direct"}</dd></div><div><dt>Connection</dt><dd>{statusText[connection?.last_check_status || "untested"]}</dd></div><div><dt>Last checked</dt><dd>{connection?.last_checked_at ? new Date(connection.last_checked_at).toLocaleString() : "Never"}</dd></div><div><dt>Connection &amp; tool access</dt><dd>{version.capabilities.join(" · ") || "None declared"}</dd></div><div><dt>Qualified</dt><dd>—</dd></div></dl>
        <p className={styles.endpoint}>{version.endpoint_url}</p>
        <div><h4>Capability claims</h4><p className={styles.endpoint}>Claims are not qualifications. TESTED means the coverage contract was satisfied by valid evidence; it does not mean the agent succeeded or is qualified.{version.capability_catalog_version ? " Catalog: " + version.capability_catalog_version : ""}</p>
          {versionClaims.length === 0 ? <p className={styles.endpoint}>No capability claims declared.</p> : <ul>{versionClaims.map((claim) => {
            const detail = coverageFor(version.id, claim.capability_id);
            // TESTED is exclusively the Stage-27 capability-coverage verdict.
            // Claim presence and claim projections can never promote it.
            const testStatus = (detail?.test_status || "untested").toLowerCase();
            const missing = detail?.missing_requirements || [];
            const exercised = detail ? detail.covered_family_count + " / " + detail.required_family_count + " task families exercised · " + detail.valid_trial_count + " valid trials" : null;
            return <li key={claim.capability_id}><strong>{claim.display_name || claim.capability_id}</strong>{claim.domain ? " — " + claim.domain + (claim.family ? " / " + claim.family : "") : ""} <span>CLAIMED</span> <span>{testStatusText[testStatus] || "UNTESTED"}</span><br /><code>{claim.capability_id}</code> <span>Qualified —</span>
              {detail ? <><br /><span>{exercised}</span>
                {detail.families.filter((family) => family.required).map((family) => <span key={family.family_id}><br />{family.covered ? "✓ " : "— "}{family.family_id} · {family.valid_trials} valid trials{family.case_types_exercised.length > 0 ? " · " + family.case_types_exercised.join("/") : ""}</span>)}
                {missing.length > 0 ? <span><br />Missing: {missing.map((requirement) => requirement.family_id || requirement.code).join(", ")}</span> : null}</> : null}
            </li>;
          })}</ul>}
        </div>
        <details><summary>Legacy qualification planning</summary><div><h4>Qualification</h4><p className={styles.endpoint}>This downstream policy workflow remains available for compatibility. It is not the primary verification result.</p>
          {(() => {
            const plan = plans[version.id];
            const failure = planError[version.id];
            if (!plan && !failure) {
              return <div className={styles.versionActions}><button type="button" onClick={() => buildPlan(version)} disabled={planBusy !== ""}>{planBusy === "plan-" + version.id ? "Building…" : "Build qualification plan"}</button></div>;
            }
            return <>
              {failure ? <p className={styles.endpoint}>{failure}</p> : null}
              {plan ? <>
                <p className={styles.endpoint}>Readiness: <strong>{planStateText[plan.state] || plan.state}</strong>{plan.generated_suite_version_id ? " · Suite: " + plan.generated_suite_version_id : ""}</p>
                <ul>{plan.capabilities.map((capability) => <li key={capability.capability_id}>
                  <code>{capability.capability_id}</code> <span>{capability.satisfied ? "satisfiable" : "blocked"}</span>
                  <br /><span>{capability.covered_families.length} / {capability.required_families.length} task families · {capability.selected_tasks.reduce((total, task) => total + task.trial_count, 0)} trials required</span>
                  {capability.blockers.length > 0 ? capability.blockers.map((blocker, index) => <span key={blocker.code + index}><br />{blocker.code}{blocker.detail ? ": " + blocker.detail : ""}</span>) : null}
                </li>)}</ul>
                {plan.blockers.filter((blocker) => !blocker.capability_id).map((blocker, index) => <p className={styles.endpoint} key={blocker.code + index}>{blocker.code}{blocker.detail ? ": " + blocker.detail : ""}</p>)}
                <div className={styles.versionActions}>
                  <button type="button" onClick={() => buildPlan(version)} disabled={planBusy !== ""}>{planBusy === "plan-" + version.id ? "Rebuilding…" : "Rebuild qualification plan"}</button>
                  {plan.state === "ready" && !plan.generated_suite_version_id ? <button type="button" onClick={() => buildSuite(version.id)} disabled={planBusy !== ""}>{planBusy === "suite-" + version.id ? "Creating…" : "Create Qualification Suite"}</button> : null}
                </div>
                {plan.generated_suite_version_id ? <p className={styles.endpoint}>Qualification Suite created. Trials have not been launched. {plan.generated_suite_id ? <a href={`/qualification/suites/${plan.generated_suite_id}`}>Review and start evaluation</a> : null}</p> : null}
              </> : null}
            </>;
          })()}
        </div></details>
        <div><h4>Verification history</h4><p className={styles.endpoint}>Repeated verification evidence under reliability-metrics-contract:v1. Consistency (pass^k: all k attempts succeed) is the primary signal; at-least-once (pass@k) is secondary. Platform-invalid trials never count as agent failures.</p>
          {(() => {
            const report = reliability[version.id];
            if (!report) return <p className={styles.endpoint}>No reliability evidence yet.</p>;
            return <>
              <p className={styles.endpoint}>Valid trials: {report.summary.empirical.valid_trials} · Pass rate: {formatPct(report.summary.empirical.pass_rate)} · Consistency (pass^3): {kEstimate(report.summary.k_metrics, 3, "pass_to_the_k")} · Sample: {sufficiencyText[report.summary.sample_sufficiency] || report.summary.sample_sufficiency} · Platform-invalid: {report.summary.empirical.platform_invalid_trials}</p>
              {report.capabilities.length === 0 ? <p className={styles.endpoint}>No claimed capabilities with reliability evidence.</p> : <ul>{report.capabilities.map((capability) => <li key={capability.capability_id}>
                <strong>{capability.capability_id}</strong> <span>{reliabilityStateText[capability.reliability_state] || capability.reliability_state}</span>
                <br /><span>Valid trials: {capability.summary.empirical.valid_trials} · Pass rate: {formatPct(capability.summary.empirical.pass_rate)} · Consistency (pass^3): {kEstimate(capability.summary.k_metrics, 3, "pass_to_the_k")} · At-least-once (pass@3): {kEstimate(capability.summary.k_metrics, 3, "pass_at_k")}</span>
                <br /><span>Family breadth: {capability.covered_families.length} / {capability.required_families.length} required families · Contrast trials: {capability.contrast_trials}{capability.contrast_pass_rate !== undefined && capability.contrast_pass_rate !== null ? " · Contrast pass rate: " + formatPct(capability.contrast_pass_rate) : ""} · Platform-invalid: {capability.summary.empirical.platform_invalid_trials}</span>
              </li>)}</ul>}
              {report.executions.length > 0 ? <><p className={styles.endpoint}>Trial evidence by suite execution (valid vs platform-invalid, consistency, sufficiency, family breadth):</p><ul>{report.executions.map((execution) => <li key={execution.suite_execution_id}>
                <code>{execution.suite_execution_id.slice(0, 8)}</code><span> Valid: {execution.summary.empirical.valid_trials} · Invalid: {execution.platform_invalid_trials} · Pass rate: {formatPct(execution.summary.empirical.pass_rate)} · Consistency (pass^3): {kEstimate(execution.summary.k_metrics, 3, "pass_to_the_k")} · At-least-once (pass@3): {kEstimate(execution.summary.k_metrics, 3, "pass_at_k")} · {sufficiencyText[execution.summary.sample_sufficiency] || execution.summary.sample_sufficiency} · Families: {execution.distinct_families} · Variants: {execution.distinct_variants}</span>
              </li>)}</ul></> : null}
            </>;
          })()}
        </div>
		<div className={styles.versionActions}><button type="button" onClick={() => testConnection(version.id)} disabled={busy !== ""}>{busy === "test-" + version.id ? "Testing…" : "Test connection"}</button><input aria-label={"New bearer token for " + version.version_label} type="password" autoComplete="new-password" placeholder="New bearer token" value={rotation[version.id] || ""} onChange={(event) => setRotation((current) => ({ ...current, [version.id]: event.target.value }))} /><button type="button" onClick={() => rotate(version.id)} disabled={busy !== "" || !rotation[version.id]}>{busy === "rotate-" + version.id ? "Updating…" : "Update credential"}</button></div>
      </article>; })}</div>
    </section>
  </main>;
}
