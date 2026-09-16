import type { Metadata } from "next";
import { ScenarioDirectory } from "./scenario-directory";
import { PublicNavigation } from "../site-navigation";
import styles from "./scenarios.module.css";

export const metadata: Metadata = { title: "Infrastructure Qualification Tasks — Evalplane", description: "Browse controlled operational tasks used to establish infrastructure capability coverage." };

export default function ScenariosPage() {
  return <main className={styles.page}>
    <PublicNavigation active="tasks" />
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>TASK CATALOG</p><h1>Evaluation tasks</h1><p>Controlled operational problems used to establish infrastructure capability coverage.</p><a className={styles.primaryAction} href="/scenarios/new">Define task</a></header>
    <ScenarioDirectory />
  </main>;
}
