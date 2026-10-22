"use client";

import { useCallback, useEffect, useState } from "react";
import { APIError, listCustomerKubernetesEnvironments, type CustomerKubernetesEnvironment } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import { ProductNavigation } from "../site-navigation";
import styles from "../product.module.css";

export default function AssurancePage() {
  const { workspace, loading: authLoading, error: authError, retry: retryAuth } = useAuth();
  const [items,setItems]=useState<CustomerKubernetesEnvironment[]>([]);
  const [loading,setLoading]=useState(true);const [error,setError]=useState("");const [attempt,setAttempt]=useState(0);
  const retry=useCallback(()=>setAttempt(v=>v+1),[]);
  useEffect(()=>{if(!workspace)return;let active=true;setLoading(true);setError("");listCustomerKubernetesEnvironments(workspace.id).then(r=>{if(active)setItems(r.items)}).catch(reason=>{if(active)setError(reason instanceof APIError?reason.message:"Assurance environments could not be loaded.")}).finally(()=>{if(active)setLoading(false)});return()=>{active=false}},[workspace,attempt]);
  return <><ProductNavigation active="assurance"/><main className={styles.page}>
    <header className={styles.header}><div><p className={styles.eyebrow}>CUSTOMER ASSURANCE</p><h1>Assurance</h1><p>Inspect operating requirements, observed evidence, historical assessments, and trust limitations for registered environments.</p></div></header>
    {(authLoading||loading)&&<p className={styles.state} role="status">Loading environments…</p>}
    {(authError||error)&&<div className={styles.error} role="alert"><p>{authError||error}</p><button onClick={authError?retryAuth:retry}>Try again</button></div>}
    {!authLoading&&!loading&&!authError&&!error&&<section className={styles.section}><h2>Registered environments</h2>{items.length===0?<div className={styles.empty}><h2>No customer environments yet</h2><p>Register a scoped Kubernetes environment before creating operating requirements.</p><a href="/environments">Open environments →</a></div>:<div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>Environment</th><th>Tier</th><th>Collector</th><th>Scope</th><th></th></tr></thead><tbody>{items.map(item=><tr key={item.id}><td><strong>{item.name}</strong><br/><span className={styles.muted}>{item.cluster_identifier}</span></td><td>{item.environment_tier}</td><td>{item.connection_status}</td><td>{item.namespaces.join(", ")}</td><td><a href={`/assurance/${item.id}`}>Inspect assurance →</a></td></tr>)}</tbody></table></div>}</section>}
  </main></>;
}
