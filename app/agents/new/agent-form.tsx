"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { APIError, apiRequest, type Agent, type AgentCapability, type AgentVersion, type CapabilityCatalogResponse, type CompetencyCapability } from "../../../lib/api";
import { useAuth } from "../../../lib/auth";
import styles from "./agent-form.module.css";

type Check = { status: string; diagnostic_code: string };
const checkCopy: Record<string, string> = {
  healthy: "Connection healthy",
  authentication_failed: "Authentication rejected. Check the bearer token.",
  unreachable: "Endpoint unreachable. Check the URL and network access.",
  protocol_error: "Protocol response invalid. Confirm the HTTP Agent API contract.",
  timeout: "Connection timed out. Confirm the endpoint responds promptly.",
  configuration_error: "Endpoint blocked by network policy or incorrectly configured.",
};

function slugify(value: string) {
  return value.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 80);
}

export function AgentForm({ existingAgentID }: { existingAgentID: string | null }) {
  const router = useRouter();
  const { workspace, loading: authLoading, error: authError, retry } = useAuth();
  const [catalog, setCatalog] = useState<AgentCapability[]>([]);
  const [competencyCatalog, setCompetencyCatalog] = useState<CompetencyCapability[]>([]);
  const [competencyContract, setCompetencyContract] = useState("infrastructure-capability-catalog:v1");
  const [name, setName] = useState("");
  const [version, setVersion] = useState("");
  const [description, setDescription] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [toolAccessMode, setToolAccessMode] = useState<"direct" | "mcp">("direct");
  const [authType, setAuthType] = useState<"none" | "bearer">("none");
  const [token, setToken] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [claimed, setClaimed] = useState<string[]>([]);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [check, setCheck] = useState<Check | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    apiRequest<{ items: AgentCapability[] }>("/v1/agent-capabilities")
      .then((value) => setCatalog(value.items))
      .catch(() => setError("Capability catalog could not be loaded."));
  }, []);

  useEffect(() => {
    if (!workspace) return;
    apiRequest<CapabilityCatalogResponse>("/v1/capability-catalog")
      .then((value) => {
        setCompetencyCatalog(value.items.filter((item) => item.status === "active"));
        setCompetencyContract(value.contract || "infrastructure-capability-catalog:v1");
      })
      .catch(() => setError("Claimed-capability catalog could not be loaded."));
  }, [workspace]);

  const grouped = useMemo(() => ({
    observations: catalog.filter((capability) => capability.kind === "observation" && capability.id !== "kubernetes"),
    actions: catalog.filter((capability) => capability.kind === "action"),
  }), [catalog]);

  const claimedHierarchy = useMemo(() => {
    const domains: { domain: string; families: { family: string; items: CompetencyCapability[] }[] }[] = [];
    const domainIndex: Record<string, number> = {};
    for (const item of competencyCatalog) {
      let idx = domainIndex[item.domain];
      if (idx === undefined) {
        idx = domains.length;
        domainIndex[item.domain] = idx;
        domains.push({ domain: item.domain, families: [] });
      }
      const families = domains[idx].families;
      let family = families.find((entry) => entry.family === item.family);
      if (!family) {
        family = { family: item.family, items: [] };
        families.push(family);
      }
      family.items.push(item);
    }
    for (const domain of domains) {
      for (const family of domain.families) {
        family.items.sort((a, b) => a.id.localeCompare(b.id));
      }
    }
    return domains;
  }, [competencyCatalog]);

  function connectionChanged() { setCheck(null); setError(""); }
  function toggle(id: string) {
    setSelected((values) => values.includes(id) ? values.filter((value) => value !== id) : [...values, id]);
  }
  function toggleClaim(id: string) {
    setClaimed((values) => values.includes(id) ? values.filter((value) => value !== id) : [...values, id]);
  }

  async function testConnection() {
    if (!workspace) return;
    setTesting(true); setError(""); setCheck(null);
    try {
      setCheck(await apiRequest<Check>("/v1/workspaces/" + workspace.id + "/agent-connections/test", {
        method: "POST",
        body: JSON.stringify({ endpoint_url: endpoint, auth_type: authType, bearer_token: authType === "bearer" ? token : "" }),
      }));
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : "Connection could not be tested.");
    } finally { setTesting(false); }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!workspace || check?.status !== "healthy") {
      setError("Test a healthy connection before registering this version.");
      return;
    }
    setSaving(true); setError("");
    try {
	  let agentID = existingAgentID;
	  let versionID: string | undefined;
	  if (existingAgentID) {
		const createdVersion = await apiRequest<AgentVersion>("/v1/workspaces/" + workspace.id + "/agents/" + existingAgentID + "/versions", {
		  method: "POST", body: JSON.stringify({ version_label: version, interface_kind: "http", tool_access_mode: toolAccessMode, endpoint_url: endpoint, capabilities: selected, claimed_capability_ids: claimed, metadata: {} }),
		});
		versionID = createdVersion.id;
	  } else {
		const created = await apiRequest<Agent>("/v1/workspaces/" + workspace.id + "/agents", {
		  method: "POST", body: JSON.stringify({ name, slug: slugify(name), description, visibility: "workspace", initial_version: { version_label: version, interface_kind: "http", tool_access_mode: toolAccessMode, endpoint_url: endpoint, capabilities: selected, claimed_capability_ids: claimed, metadata: {} } }),
		});
		agentID = created.id;
		versionID = created.latest_version?.id;
	  }
      if (!versionID) throw new APIError(0, "registration_error", "The agent version was not returned.");
      await apiRequest("/v1/workspaces/" + workspace.id + "/agent-versions/" + versionID + "/credential", {
		method: "PUT",
		body: JSON.stringify({ auth_type: authType, bearer_token: authType === "bearer" ? token : "" }),
	  });
	  await apiRequest("/v1/workspaces/" + workspace.id + "/agent-versions/" + versionID + "/connection/test", { method: "POST", body: "{}" });
      setToken("");
	  router.push("/agents/" + agentID);
      router.refresh();
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : "The agent could not be registered.");
    } finally { setSaving(false); }
  }

  if (authLoading) return <main className={styles.page}><p className={styles.state}>Loading workspace…</p></main>;
  if (authError || !workspace) return <main className={styles.page}><div className={styles.state}><p>{authError || "A workspace is required."}</p><button onClick={retry}>Try again</button></div></main>;

  return <main className={styles.page}>
    <header className={styles.nav}><a href="/">evalplane</a><a href="/agents">Agents</a></header>
    <div className={styles.shell}>
	  <p className={styles.eyebrow}>{existingAgentID ? "REGISTER VERSION" : "CONNECT AN AGENT"}</p>
	  <h1>{existingAgentID ? "Register a new agent version." : "Connect an infrastructure agent."}</h1>
      <p className={styles.lead}>Connect an infrastructure agent and qualify the capabilities it claims to support. Register an exact behavioral version. Credentials can be rotated later without changing its identity.</p>
      <form onSubmit={submit} className={styles.form}>
		<section>
          <h2>Identity and version</h2>
		  {!existingAgentID && <label>Agent name<input required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} /></label>}
          <label>Version<input required maxLength={100} placeholder="1.0.0" value={version} onChange={(event) => setVersion(event.target.value)} /></label>
		  {!existingAgentID && <label>Description <span>Optional</span><textarea maxLength={4000} value={description} onChange={(event) => setDescription(event.target.value)} /></label>}
		</section>
        <section>
          <h2>Connection &amp; tool access</h2>
          <p className={styles.explanation}>Endpoint, authentication, and low-level runtime capabilities control compatibility and tool exposure. These are not qualifications.</p>
          <div className={styles.methods}>
            <label><input type="radio" checked readOnly /> HTTP Agent API <strong>Available</strong></label>
          </div>
          <label>Endpoint URL<input required type="url" placeholder="https://agent.example.com/v1/decide" value={endpoint} onChange={(event) => { setEndpoint(event.target.value); connectionChanged(); }} /></label>
          <fieldset className={styles.auth}><legend>Authentication</legend>
            <label><input type="radio" checked={authType === "none"} onChange={() => { setAuthType("none"); connectionChanged(); }} /> None</label>
            <label><input type="radio" checked={authType === "bearer"} onChange={() => { setAuthType("bearer"); connectionChanged(); }} /> Bearer token</label>
          </fieldset>
          {authType === "bearer" && <label>Bearer token<input required type="password" autoComplete="new-password" value={token} onChange={(event) => { setToken(event.target.value); connectionChanged(); }} /></label>}
          <button className={styles.secondary} type="button" onClick={testConnection} disabled={testing || !endpoint || (authType === "bearer" && !token)}>{testing ? "Testing…" : "Test connection"}</button>
          {check && <p className={check.status === "healthy" ? styles.healthy : styles.check}>{checkCopy[check.status] || "Connection check completed."}</p>}
          <div className={styles.methods}>
            <label><input type="radio" checked={toolAccessMode === "direct"} onChange={() => setToolAccessMode("direct")} /> Direct Evalplane action protocol</label>
            <label><input type="radio" checked={toolAccessMode === "mcp"} onChange={() => setToolAccessMode("mcp")} /> MCP <strong>Available</strong></label>
          </div>
          {toolAccessMode === "mcp" && <p className={styles.explanation}>Evalplane invokes your agent over its registered endpoint and provides a short-lived MCP connection for evaluation-scoped observations and actions.</p>}
          {Object.entries(grouped).map(([group, items]) => <fieldset className={styles.capabilities} key={group}><legend>{group}</legend>{items.map((item) => <label key={item.id}><input type="checkbox" checked={selected.includes(item.id)} onChange={() => toggle(item.id)} /><span>{item.label}</span><code>{item.id}</code></label>)}</fieldset>)}
        </section>
        <section>
          <h2>Claimed capabilities</h2>
          <p className={styles.explanation}>What can this agent operate? Select what this exact version CLAIMS it can do. Claims are not qualifications. Evalplane must test them before they become qualified. Catalog: <code>{competencyContract}</code>.</p>
          {claimedHierarchy.length === 0 && <p className={styles.explanation}>Claimed-capability catalog could not be loaded yet. Runtime capabilities above can still be registered; claims stay empty.</p>}
          {claimedHierarchy.map((domain) => <div key={domain.domain}>
            <h3>{domain.domain}</h3>
            {domain.families.map((group) => <fieldset className={styles.capabilities} key={group.family}><legend>{group.family}</legend>{group.items.map((item) => <label key={item.id}><input type="checkbox" checked={claimed.includes(item.id)} onChange={() => toggleClaim(item.id)} /><span>{item.display_name}</span><code>{item.id}</code></label>)}</fieldset>)}
          </div>)}
        </section>
        {error && <p className={styles.error} role="alert">{error}</p>}
		<div className={styles.actions}><a href="/agents">Cancel</a><button type="submit" disabled={saving || check?.status !== "healthy"}>{saving ? "Registering…" : existingAgentID ? "Register version" : "Register agent"}</button></div>
      </form>
    </div>
  </main>;
}
