import type { Metadata } from "next";
import { ProductNavigation } from "../../site-navigation";
import { ScenarioForm } from "./scenario-form";
import styles from "./new-scenario.module.css";

export const metadata: Metadata = { title: "Define an evaluation task — Evalplane", description: "Define a custom infrastructure evaluation task." };

export default function NewScenarioPage() {
  return <main className={styles.page}><ProductNavigation active="scenarios" /><header className={`${styles.intro} ${styles.shell}`}><p className={styles.label}>CUSTOM SCENARIO</p><h1>Define a verification scenario</h1><p>Describe the infrastructure condition, protected resources, agent objective, and verification expectations.</p></header><ScenarioForm /></main>;
}
