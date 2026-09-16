import type { Metadata } from "next";
import { ProductNavigation } from "../../site-navigation";
import { ScenarioForm } from "./scenario-form";
import styles from "./new-scenario.module.css";

export const metadata: Metadata = { title: "Define an evaluation task — Evalplane", description: "Define a custom infrastructure evaluation task." };

export default function NewScenarioPage() {
  return <main className={styles.page}><ProductNavigation active="tasks" /><header className={`${styles.intro} ${styles.shell}`}><p className={styles.label}>CUSTOM TASK</p><h1>Define an evaluation task</h1><p>Describe the conditions, expected behavior, and qualification criteria.</p></header><ScenarioForm /></main>;
}
