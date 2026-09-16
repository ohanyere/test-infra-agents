"use client";

import { useEffect, useState } from "react";
import { APIError, getBilling, getEntitlements, getUsage, type BillingResponse, type EntitlementsResponse, type UsageResponse } from "../../../lib/api";
import { useAuth } from "../../../lib/auth";
import styles from "./billing.module.css";

export default function BillingPage() {
  const { workspace } = useAuth();
  const [billing, setBilling] = useState<BillingResponse>();
  const [entitlements, setEntitlements] = useState<EntitlementsResponse>();
  const [usage, setUsage] = useState<UsageResponse>();
  const [error, setError] = useState("");
  useEffect(() => {
    if (!workspace) return;
    Promise.all([getBilling(workspace.id), getEntitlements(workspace.id), getUsage(workspace.id)])
      .then(([b, e, u]) => { setBilling(b); setEntitlements(e); setUsage(u); })
      .catch((reason: unknown) => setError(reason instanceof APIError ? reason.message : "Billing information is unavailable."));
  }, [workspace]);
  if (error) return <main className={styles.page}><p role="alert">{error}</p></main>;
  if (!billing || !entitlements || !usage) return <main className={styles.page}><p role="status">Loading billing and usage…</p></main>;
  const plan = billing.plans.find((item) => item.id === billing.account.plan_id);
  const period = `${new Date(usage.usage.period_start).toLocaleDateString()} – ${new Date(usage.usage.period_end).toLocaleDateString()}`;
  return <main className={styles.page}>
    <header><p className={styles.eyebrow}>WORKSPACE SETTINGS</p><h1>Billing &amp; usage</h1><p>Plan access and evaluation consumption are enforced by the server.</p></header>
    <section className={styles.card}><div><span>Current plan</span><strong>{plan?.display_name ?? billing.account.plan_id}</strong></div><div><span>Billing state</span><strong>{billing.account.status.replaceAll("_", " ")}</strong></div><div><span>Current period</span><strong>{period}</strong></div><button disabled={!billing.provider_backed} title={!billing.provider_backed ? "Billing automation is not enabled in this environment" : undefined}>{billing.provider_backed ? "Manage billing" : "Billing automation unavailable"}</button></section>
    <section><h2>Evaluation credits</h2><div className={styles.metrics}><div><span>Included</span><strong>{usage.usage.allowance_label}</strong></div><div><span>Used</span><strong>{usage.usage.committed}</strong></div><div><span>Reserved</span><strong>{usage.usage.reserved}</strong></div><div><span>Available</span><strong>{usage.usage.unlimited ? "Custom" : usage.usage.available}</strong></div></div></section>
    <section><h2>Plan entitlements</h2><ul className={styles.entitlements}>{entitlements.items.map((item) => <li key={item.key}><span>{item.key.replaceAll("_", " ")}</span><strong className={item.allowed ? styles.allowed : styles.denied}>{item.allowed ? item.limit_label || item.value || "Included" : "Not included"}</strong><small>{item.reason}</small></li>)}</ul></section>
    <section><h2>Recent usage</h2>{usage.usage.recent_entries.length === 0 ? <p>No usage has been recorded this period.</p> : <ul className={styles.ledger}>{usage.usage.recent_entries.map((entry) => <li key={entry.id}><strong>{entry.kind}</strong><span>{entry.quantity} credit{entry.quantity === 1 ? "" : "s"}</span><time>{new Date(entry.created_at).toLocaleString()}</time></li>)}</ul>}</section>
  </main>;
}
