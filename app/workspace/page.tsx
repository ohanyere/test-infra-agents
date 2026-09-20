"use client";

import { useEffect, useState } from "react";
import { APIError, getVerification, listAgents, listBatches, listBatchTrials, type Agent, type EvaluationBatch, type Trial, type VerificationResult } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import styles from "../product.module.css";

type VerificationRow = { batch: EvaluationBatch; trial: Trial; verification?: VerificationResult };
function status(row: VerificationRow) { if (row.verification) return row.verification.overall_status; if (["completed", "failed", "stopped"].includes(row.trial.status)) return "LEGACY"; return row.trial.status.toUpperCase(); }
function statusClass(result: string) { if (result === "VERIFIED") return styles.verifiedStatus; if (result === "FAILED") return styles.failedStatus; return styles.inconclusiveStatus; }

export default function WorkspacePage() {
  const { workspace } = useAuth();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [rows, setRows] = useState<VerificationRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!workspace) return;
    Promise.all([listAgents(workspace.id), listBatches(workspace.id, 8)]).then(async ([agentPage, batchPage]) => {
      setAgents(agentPage.items);
      const pages = await Promise.all(batchPage.items.map(async (batch) => ({ batch, trials: (await listBatchTrials(workspace.id, batch.id)).items })));
      const recent = pages.flatMap((item) => item.trials.map((trial) => ({ batch: item.batch, trial }))).sort((a, b) => Date.parse(b.trial.created_at) - Date.parse(a.trial.created_at)).slice(0, 8);
      const hydrated = await Promise.all(recent.map(async (row) => {
        if (!["completed", "failed", "stopped"].includes(row.trial.status)) return row;
        try { return { ...row, verification: await getVerification(workspace.id, row.trial.id) }; } catch { return row; }
      }));
      setRows(hydrated);
    }).catch((reason: unknown) => setError(reason instanceof APIError ? reason.message : "Workspace state could not be loaded.")).finally(() => setLoading(false));
  }, [workspace]);
  const authoritative = rows.filter((row) => row.verification);
  const counts = Object.fromEntries(["VERIFIED", "WARNING", "FAILED", "INCONCLUSIVE"].map((result) => [result, authoritative.filter((row) => status(row) === result).length]));
  const issues = authoritative.filter((row) => ["WARNING", "FAILED", "INCONCLUSIVE"].includes(status(row))).flatMap((row) => {
    const verification = row.verification!;
    return [...verification.infrastructure_diff.filter((change) => change.classification !== "EXPECTED").map((change) => `${change.classification}: ${change.resource}`), ...verification.side_effects.findings.map((finding) => finding.summary), ...(verification.stability.status !== "STABLE" ? [verification.stability.reason] : []), ...verification.forward_risks.map((risk) => risk.reason)];
  }).slice(0, 6);
  return <main className={styles.page}>
    <header className={styles.header}><div><p className={styles.eyebrow}>AUTONOMOUS INFRASTRUCTURE VERIFICATIONS</p><h1>{workspace?.name}</h1><p>Understand the quality and consequences of the resulting states autonomous infrastructure changes produced.</p></div><div className={styles.actions}><a href="/agents/new">Connect agent</a><a className={styles.secondary} href="/verifications">All verifications</a></div></header>
    {loading && <p className={styles.state}>Loading workspace…</p>}{error && <p className={styles.error} role="alert">{error}</p>}
    {!loading && !error && <>
      <section className={styles.section}><h2>Verification results</h2>{authoritative.length === 0 ? <p className={styles.muted}>No authoritative VerificationResult records are available yet. Legacy runs are not counted as verified.</p> : <div className={styles.metrics}>{["VERIFIED", "WARNING", "FAILED", "INCONCLUSIVE"].map((result) => <div key={result}><span>{result}</span><strong className={statusClass(result)} style={result === "WARNING" ? { color: "#9a6818" } : undefined}>{counts[result]}</strong></div>)}</div>}</section>
      <section className={styles.section}><h2>Resulting-state issues</h2>{issues.length === 0 ? <p className={styles.muted}>No supported issue was found in the loaded authoritative results.</p> : <ul className={styles.list}>{issues.map((issue, index) => <li key={`${issue}-${index}`}><div><strong>{issue}</strong></div></li>)}</ul>}</section>
      <section className={styles.section}><h2>Recent verifications</h2>{rows.length === 0 ? <div className={styles.empty}><h2>No verifications yet.</h2><p>Connect an agent and run a controlled infrastructure test to inspect the resulting state.</p><a href="/agents">Open agents</a></div> : <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>Result</th><th>Agent</th><th>Version</th><th>Objective / test</th><th>Created</th><th /></tr></thead><tbody>{rows.map((row) => { const result = status(row); return <tr key={row.trial.id}><td><span className={`${styles.status} ${statusClass(result)}`} style={result === "WARNING" ? { color: "#9a6818" } : undefined}>{result}</span></td><td>{row.trial.agent.name}</td><td>{row.trial.agent.version_label}</td><td>{row.verification?.intent.objective || row.trial.scenario?.name || row.batch.scenario.name}</td><td>{new Date(row.trial.created_at).toLocaleString()}</td><td><a href={`/verifications/${row.trial.id}`}>View</a></td></tr>; })}</tbody></table></div>}</section>
      <section className={styles.section}><h2>Agents</h2>{agents.length === 0 ? <p className={styles.muted}>No agents connected.</p> : <ul className={styles.list}>{agents.slice(0, 5).map((agent) => <li key={agent.id}><div><strong>{agent.name}</strong><p>{agent.latest_version ? `Version ${agent.latest_version.version_label}` : "No registered version"}</p></div><span>Inspect resulting-state history</span><a href={`/agents/${agent.id}`}>Open</a></li>)}</ul>}</section>
    </>}
  </main>;
}
