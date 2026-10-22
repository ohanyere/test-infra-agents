"use client";

import { useCallback, useEffect, useState } from "react";
import { APIError, apiRequest, listAgents, listCustomerKubernetesEnvironments, listWorkspaceScenarios, registerCustomerKubernetesEnvironment, startCustomerObservation, rotateCustomerCollector, revokeCustomerEnvironment, type Agent, type CustomerKubernetesEnvironment, type CustomerObservationSession, type CustomerScenario, type EnvironmentInstance, type EnvironmentProfile, type EnvironmentProvider, type ListResponse } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import { ProductNavigation } from "../site-navigation";
import styles from "../product.module.css";
import environmentStyles from "./environments.module.css";

export default function EnvironmentsPage() {
  const { workspace, loading: authLoading, error: authError, retry: retryAuth } = useAuth();
  const [profiles, setProfiles] = useState<EnvironmentProfile[]>([]);
  const [providers, setProviders] = useState<EnvironmentProvider[]>([]);
  const [instances, setInstances] = useState<EnvironmentInstance[]>([]);
  const [customerEnvironments, setCustomerEnvironments] = useState<CustomerKubernetesEnvironment[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [scenarios, setScenarios] = useState<CustomerScenario[]>([]);
  const [credential, setCredential] = useState("");
  const [workflowMessage, setWorkflowMessage] = useState("");
  const [lastRunID, setLastRunID] = useState("");
  const [lastSession, setLastSession] = useState<CustomerObservationSession | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(() => setAttempt((value) => value + 1), []);

  useEffect(() => {
    if (!workspace) return;
    let active = true;
    setLoading(true);
    setError("");
    Promise.all([
      apiRequest<ListResponse<EnvironmentProfile>>("/v1/environment-profiles"),
      apiRequest<ListResponse<EnvironmentProvider>>("/v1/environment-providers"),
      apiRequest<ListResponse<EnvironmentInstance>>(`/v1/workspaces/${workspace.id}/environments?limit=50&offset=0`),
      listCustomerKubernetesEnvironments(workspace.id),
      listAgents(workspace.id),
      listWorkspaceScenarios(workspace.id),
    ]).then(([profileResponse, providerResponse, instanceResponse, customerResponse, agentResponse, scenarioResponse]) => {
      if (!active) return;
      setProfiles(profileResponse.items);
      setProviders(providerResponse.items);
      setInstances(instanceResponse.items);
      setCustomerEnvironments(customerResponse.items);
      setAgents(agentResponse.items);
      setScenarios(scenarioResponse.items);
    }).catch((reason: unknown) => {
      if (active) setError(reason instanceof APIError ? reason.message : "Environment inventory could not be loaded.");
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [workspace, attempt]);

  const providerByID = new Map(providers.map((provider) => [provider.id, provider]));
  async function registerEnvironment(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!workspace) return; const formElement = event.currentTarget; setBusy(true); setWorkflowMessage(""); setCredential(""); setLastRunID(""); setLastSession(null);
    const form = new FormData(formElement);
    try {
      const response = await registerCustomerKubernetesEnvironment(workspace.id, { name: String(form.get("name") || ""), cluster_identifier: String(form.get("cluster_identifier") || ""), namespaces: String(form.get("namespaces") || "").split(",").map((value) => value.trim()).filter(Boolean), environment_tier: String(form.get("environment_tier")) === "production" ? "production" : "staging", resource_kinds: ["Deployment", "Pod", ...form.getAll("audit_kind").map(String)], allow_forwarded_audit: form.get("allow_forwarded_audit") === "on" });
      setCustomerEnvironments((items) => [response.environment, ...items]); setCredential(response.collector_credential); formElement.reset(); setWorkflowMessage("Environment registered. Copy the collector credential now; it will not be shown again.");
    } catch (reason) { setWorkflowMessage(reason instanceof APIError ? reason.message : "Environment registration failed."); }
    finally { setBusy(false); }
  }
  async function startSession(event: React.FormEvent<HTMLFormElement>, environmentID: string) {
    event.preventDefault(); if (!workspace) return; const form=new FormData(event.currentTarget); setBusy(true);setWorkflowMessage("");setLastRunID("");
    try { const session=await startCustomerObservation(workspace.id,environmentID,{agent_version_id:String(form.get("agent_version_id")||""),scenario_version_id:String(form.get("scenario_version_id")||"")}); setLastRunID(session.run_id); setLastSession(session); setWorkflowMessage(`Observation session created. The collector must observe for at least ${session.observation_window_seconds} seconds. Snapshots do not establish agent causation.`); }
    catch(reason){setWorkflowMessage(reason instanceof APIError?reason.message:"Could not start observation session.");}finally{setBusy(false);}
  }
  async function rotateCredential(environmentID:string){if(!workspace)return;setBusy(true);setWorkflowMessage("");try{const response=await rotateCustomerCollector(workspace.id,environmentID);setCredential(response.collector_credential);setCustomerEnvironments(items=>items.map(item=>item.id===environmentID?response.environment:item));setWorkflowMessage("Collector credential rotated. The old credential is invalid. Replace the Secret before the next Job.");}catch(reason){setWorkflowMessage(reason instanceof APIError?reason.message:"Credential rotation failed.");}finally{setBusy(false);}}
  async function revokeEnvironment(environmentID:string){if(!workspace)return;setBusy(true);setWorkflowMessage("");try{await revokeCustomerEnvironment(workspace.id,environmentID);setCustomerEnvironments((items)=>items.map((item)=>item.id===environmentID?{...item,connection_status:"revoked"}:item));setCredential("");setWorkflowMessage("Environment and active collector sessions revoked.");}catch(reason){setWorkflowMessage(reason instanceof APIError?reason.message:"Environment revocation failed.");}finally{setBusy(false);}}
  return <><ProductNavigation active="environments" /><main className={styles.page}>
    <header className={styles.header}><div><p className={styles.eyebrow}>ENVIRONMENT INVENTORY</p><h1>Environments</h1><p>Review run-owned environments and register customer-owned Kubernetes scopes for read-only observation.</p></div></header>
    {(authLoading || loading) && <p className={styles.state} role="status">Loading environment inventory…</p>}
    {(authError || error) && <div className={styles.error} role="alert"><p>{authError || error}</p><button type="button" onClick={authError ? retryAuth : retry}>Try again</button></div>}
    {!authLoading && !loading && !error && !authError && <>
      <section className={styles.section}><h2>Run-owned environments</h2>{instances.length === 0 ? <p className={styles.muted}>No environment instances are currently associated with this workspace. Environments are provisioned for supported runs and are not persistent customer cluster connections.</p> : <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>Status</th><th>Profile</th><th>Provider</th><th>Isolation</th><th>Created</th></tr></thead><tbody>{instances.map((instance) => <tr key={instance.id}><td>{instance.status}</td><td>{profiles.find((profile) => profile.id === instance.profile_id)?.display_name || instance.profile_id}</td><td>{providerByID.get(instance.provider_id)?.display_name || instance.provider_id}</td><td>{instance.isolation_tier}</td><td>{new Date(instance.created_at).toLocaleString()}</td></tr>)}</tbody></table></div>}</section>
      <section className={styles.section}><h2>Customer-owned Kubernetes</h2><p className={styles.muted}>Register a read-only collector scope. The generated credential is returned once for an authorized operator to place in a Kubernetes Secret; this API stores only its hash. <a href="/environments/setup">Read the collector setup guide →</a></p>
        <form className={environmentStyles.collectorForm} onSubmit={registerEnvironment}><label>Name<input name="name" required maxLength={120} /></label><label>Cluster identifier<input name="cluster_identifier" required maxLength={200} placeholder="opaque customer identifier" /></label><label>Authorized namespaces<input name="namespaces" required placeholder="payments,checkout" /></label><label>Environment tier<select name="environment_tier"><option value="staging">Staging</option><option value="production">Production</option></select></label><fieldset><legend>Additional audit resource scope</legend>{["Service", "Ingress", "NetworkPolicy", "Namespace"].map(kind=><label key={kind}><input type="checkbox" name="audit_kind" value={kind}/> {kind}</label>)}<p>Only selected kinds can enter direct authenticated audit intake. Deployment and Pod are included by default.</p></fieldset><label className={environmentStyles.auditOption}><input name="allow_forwarded_audit" type="checkbox"/> Enable forwarded audit intake <span>Requires an operator-provided API-server audit export. Records remain origin-unverified candidates.</span></label><button type="submit" disabled={busy||workspace?.role==="viewer"}>Register environment</button></form>
        {workflowMessage&&<p role="status" className={styles.notice}>{workflowMessage}{lastRunID&&<> <a href={`/runs/${lastRunID}`}>Open persisted run report →</a></>}</p>}{credential&&<div className={environmentStyles.credential}><label htmlFor="collector-credential">One-time collector credential</label><textarea id="collector-credential" readOnly value={credential} rows={2}/><p>Copy this into a Kubernetes Secret. Never commit it.</p></div>}{lastSession&&<div className={environmentStyles.credential}><strong>New observation session</strong><p>Session ID: <code>{lastSession.id}</code></p><p>Use this as the <code>session-id</code> Secret value, then start the collector Job. <a href={`/assurance/${lastSession.environment_id}/sessions/${lastSession.id}`}>Inspect ingestion and assessment →</a></p></div>}
        {customerEnvironments.length===0?<p className={styles.muted}>No customer Kubernetes environments registered.</p>:<div className={environmentStyles.customerList}>{customerEnvironments.map((environment)=><article className={environmentStyles.customerEnvironment} key={environment.id}><div className={environmentStyles.customerHeading}><div><h3>{environment.name}</h3><p>{environment.cluster_identifier} · {environment.environment_tier}</p></div><span>{environment.connection_status}</span></div><p>Scope: {environment.namespaces.join(", ")} · {environment.resource_kinds.join(", ")}</p><p><a href={`/assurance/${environment.id}`}>Evidence sources, session ingestion, and reports →</a></p><div className={environmentStyles.customerActions}><button type="button" disabled={busy||environment.connection_status==="revoked"||workspace?.role==="viewer"} onClick={()=>void rotateCredential(environment.id)}>Rotate credential</button><button type="button" disabled={busy||environment.connection_status==="revoked"||workspace?.role==="viewer"} onClick={()=>void revokeEnvironment(environment.id)}>Revoke</button></div><details><summary>Start observation-only verification</summary><form className={environmentStyles.collectorForm} onSubmit={(event)=>void startSession(event,environment.id)}><label>Agent and version<select name="agent_version_id" required defaultValue=""><option value="" disabled>Select a registered agent version</option>{agents.filter((agent)=>agent.status==="active"&&agent.latest_version).map((agent)=><option key={agent.latest_version!.id} value={agent.latest_version!.id}>{agent.name} · {agent.latest_version!.version_label}</option>)}</select></label><label>Objective and criteria<select name="scenario_version_id" required defaultValue=""><option value="" disabled>Select a verification scenario</option>{scenarios.filter((scenario)=>scenario.status==="active"&&scenario.latest_version).map((scenario)=><option key={scenario.latest_version!.id} value={scenario.latest_version!.id}>{scenario.name} · v{scenario.latest_version!.version}</option>)}</select></label><button type="submit" disabled={busy||environment.connection_status==="revoked"||workspace?.role==="viewer"||agents.length===0||scenarios.length===0}>Create session</button></form>{(agents.length===0||scenarios.length===0)&&<p>Register an agent and a scenario before starting observation. <a href="/agents">Agents</a> · <a href="/scenarios">Scenarios</a></p>}<p>After creating a session, install the customer-hosted collector using the returned session ID and one-time environment credential. The report will remain INCONCLUSIVE when required telemetry or action attribution is absent.</p></details></article>)}</div>}
      </section>
      <section className={styles.section}><h2>Supported profiles</h2><div className={environmentStyles.environmentProfiles}>{profiles.map((profile) => {
        const compatibleProviders = providers.filter((provider) => provider.supported_profiles.includes(profile.id));
        return <article className={environmentStyles.environmentProfile} key={profile.id}><div><p className={styles.eyebrow}>{profile.status.replaceAll("_", " ").toUpperCase()}</p><h3>{profile.display_name}</h3><p>{profile.description}</p></div><dl><div><dt>Isolation</dt><dd>{profile.isolation_tier.replaceAll("_", " ")}</dd></div><div><dt>Providers</dt><dd>{compatibleProviders.length ? compatibleProviders.map((provider) => `${provider.display_name}${provider.development_only ? " (development only)" : ""}`).join(", ") : "No compatible provider"}</dd></div><div><dt>Contract</dt><dd>{profile.contract_version}</dd></div></dl></article>;
      })}</div><p className={styles.notice}>Ephemeral controlled-execution profiles remain development-only. Customer environments currently support read-only Kubernetes observation; they do not authorize mutations and do not independently prove agent causation.</p></section>
    </>}
  </main></>;
}
