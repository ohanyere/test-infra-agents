"use client";

import { useEffect, useState } from "react";
import { apiRequest, APIError, type Agent, type ListResponse } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import styles from "./agents.module.css";

export function AgentDirectory() {
  const { workspace, loading: authLoading, error: authError, retry } = useAuth();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!workspace) { setLoading(!authError); return; }
    let active = true;
    setLoading(true); setError("");
    apiRequest<ListResponse<Agent>>(`/v1/workspaces/${workspace.id}/agents?status=all`)
      .then((value) => { if (active) setAgents(value.items); })
      .catch((reason: unknown) => { if (active) setError(reason instanceof APIError ? reason.message : "Agents could not be loaded."); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [workspace, authError]);

  return <section className={`${styles.directory} ${styles.shell}`} aria-label="Connected agents">
    <div className={styles.directorySummary}><span>{agents.length} connected {agents.length === 1 ? "agent" : "agents"}</span><a className={styles.button} href="/agents/new">Connect an agent</a></div>
    {(authLoading || loading) && <p className={styles.emptyState}>Loading agents…</p>}
    {(authError || error) && <div className={styles.emptyState}><p>{authError || error}</p><button type="button" onClick={retry}>Try again</button></div>}
    {!authLoading && !loading && !authError && !error && agents.length === 0 && <div className={styles.emptyState}><h2>No agents connected yet.</h2><p>Register an exact AgentVersion and declare its capabilities. Qualification is earned from policy-satisfying evidence, never assumed from a claim.</p><a className={styles.button} href="/agents/new">Connect an agent</a></div>}
    <div className={styles.agentList}>{agents.map((agent) => <article className={styles.agentRow} key={agent.id}>
      <div className={styles.agentIdentity}><h2>{agent.name}</h2><p>{agent.description || "No description"}</p></div>
      <div className={styles.strongest}><span>Latest registered version · capability claims</span><p>{agent.latest_version?.version_label || "No version"} · {agent.latest_version?.interface_kind || "unconfigured"}</p><p>{agent.latest_version?.capabilities.join(" · ") || "No capability claims declared"}</p><p className={styles.qualificationNote}>Qualification status: earned through trials — no qualification data is shown until trials run.</p></div>
      <a className={styles.agentLink} href={`/agents/${agent.id}`}>View agent <span aria-hidden="true">→</span></a>
    </article>)}</div>
  </section>;
}
