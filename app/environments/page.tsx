import type { Metadata } from "next";
import styles from "../scenarios/scenarios.module.css";

export const metadata: Metadata = { title: "Environments — Evalplane", description: "Server-owned execution environment profiles and providers for evaluation tasks." };

// V1 availability truth, mirrored from the server-owned catalogs
// (environment-component-catalog:v1, environment-profile-catalog:v1,
// environment-provider-contract:v1). Only Kubernetes Namespace on the
// development runtime is available; everything else is planned or
// experimental and never faked as available.
const profiles = [
  { profile: "Kubernetes Namespace", provider: "Kind development", isolation: "Namespace", status: "Available (development)", note: "Ordinary application/workload evaluations inside a shared cluster. Development/focused testing only." },
  { profile: "Kubernetes Virtual Cluster", provider: "Virtual-cluster provider", isolation: "Virtual cluster", status: "Planned", note: "Stronger control-plane isolation without an independent cloud cluster." },
  { profile: "Dedicated Kubernetes", provider: "Dedicated-cluster provider", isolation: "Dedicated cluster", status: "Planned", note: "Node/cluster/CNI-class scenarios requiring an independent cluster lifecycle." },
  { profile: "GitOps with Argo CD", provider: "GitOps provider", isolation: "Namespace or stronger", status: "Planned", note: "GitOps drift and sync evaluations; never claimed available until a real provider exists." },
];

export default function EnvironmentsPage() {
  return <main className={styles.page}>
    <header className={styles.siteHeader}><nav className={`${styles.nav} ${styles.navShell}`} aria-label="Main navigation">
      <a className={styles.wordmark} href="/" aria-label="Evalplane home">evalplane</a>
      <div className={styles.navLinks} aria-label="Primary navigation"><a href="/#how-qualification-works">Product</a><a href="/scenarios">Evaluation Tasks</a><a className={styles.activeNavLink} href="/environments" aria-current="page">Environments</a><a href="/pricing">Pricing</a></div>
      <div className={styles.navActions}><a className={styles.signIn} href="/login">Sign in</a><a className={`${styles.button} ${styles.compactButton}`} href="/signup">Get started</a></div>
    </nav></header>
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>ENVIRONMENTS</p><h1>Where an evaluation task can validly execute.</h1><p>Each task declares infrastructure requirements; the scheduler selects the least-fidelity adequate server-owned profile and provider. Required isolation is never downgraded for availability. Workspace runs show live instances (profile, provider, isolation, status, owner, created).</p></header>
    <section className={styles.shell} aria-label="Environment profiles">
      {profiles.map((row) => <div key={row.profile}><h2>{row.profile}</h2><dl><div><dt>Provider</dt><dd>{row.provider}</dd></div><div><dt>Isolation</dt><dd>{row.isolation}</dd></div><div><dt>Status</dt><dd>{row.status}</dd></div></dl><p>{row.note}</p></div>)}
      <p>Kind is a development runtime, never production qualification infrastructure. AWS sandboxes are not presented as available.</p>
    </section>
  </main>;
}
