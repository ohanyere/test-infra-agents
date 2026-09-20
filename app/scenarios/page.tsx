import type { Metadata } from "next";
import { ScenarioDirectory } from "./scenario-directory";
import { PublicNavigation } from "../site-navigation";
import styles from "./scenarios.module.css";

export const metadata: Metadata = { title: "Infrastructure Verification Tests — Evalplane", description: "Browse controlled infrastructure test cases for resulting-state verification." };

export default function ScenariosPage() {
  return <main className={styles.page}>
    <PublicNavigation />
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>DEVELOPMENT TESTS</p><h1>Infrastructure test cases</h1><p>Supporting fixtures for creating reproducible autonomous changes and independently checking the resulting state.</p><a className={styles.primaryAction} href="/scenarios/new">Define test case</a></header>
    <ScenarioDirectory />
  </main>;
}
