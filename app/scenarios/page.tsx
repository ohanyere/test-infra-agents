import type { Metadata } from "next";
import { ScenarioDirectory } from "./scenario-directory";
import { PublicNavigation } from "../site-navigation";
import styles from "./scenarios.module.css";

export const metadata: Metadata = { title: "Infrastructure Verification Scenarios — Evalplane", description: "Browse controlled infrastructure situations for independent agent verification." };

export default function ScenariosPage() {
  return <main className={styles.page}>
    <PublicNavigation active="scenarios" />
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>SCENARIO CATALOG</p><h1>Infrastructure scenarios</h1><p>Controlled infrastructure conditions for verifying agent outcomes, side effects, and stability.</p><a className={styles.primaryAction} href="/scenarios/new">Define scenario</a></header>
    <ScenarioDirectory />
  </main>;
}
