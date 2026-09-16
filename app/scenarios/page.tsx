import type { Metadata } from "next";
import { ScenarioDirectory } from "./scenario-directory";
import { PublicNavigation } from "../site-navigation";
import styles from "./scenarios.module.css";

export const metadata: Metadata = { title: "Evaluation Tasks — Evalplane", description: "Browse evaluation tasks grouped by task family for qualifying infrastructure agents." };

export default function ScenariosPage() {
  return <main className={styles.page}>
    <PublicNavigation active="tasks" />
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>TASK CATALOG</p><h1>Evaluation tasks</h1><p>Real operational problems used to test infrastructure agents.</p><a className={styles.primaryAction} href="/scenarios/new">Define task</a></header>
    <ScenarioDirectory />
  </main>;
}
