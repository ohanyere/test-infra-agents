import type { Metadata } from "next";
import styles from "../scenarios/scenarios.module.css";
import { PublicNavigation } from "../site-navigation";
import { SiteFooter } from "../site-footer";

export const metadata: Metadata = { title: "Environments — Realverax", description: "Server-owned execution environment profiles and providers for evaluation tasks." };

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
    <PublicNavigation />
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>ENVIRONMENTS</p><h1>Execution environments</h1><p>Server-owned profiles matched to each task&apos;s infrastructure and isolation requirements.</p></header>
    <section className={styles.shell} aria-label="Environment profiles">
      {profiles.map((row) => <div key={row.profile}><h2>{row.profile}</h2><dl><div><dt>Provider</dt><dd>{row.provider}</dd></div><div><dt>Isolation</dt><dd>{row.isolation}</dd></div><div><dt>Status</dt><dd>{row.status}</dd></div></dl><p>{row.note}</p></div>)}
      <p>Kind is a development runtime, never production qualification infrastructure. AWS sandboxes are not presented as available.</p>
    </section>
    <SiteFooter />
  </main>;
}
