import type { Metadata } from "next";
import { ProductNavigation } from "../../site-navigation";
import { ScenarioForm } from "./scenario-form";
import styles from "./new-scenario.module.css";

export const metadata: Metadata = { title: "Define an infrastructure test — Evalplane", description: "Define a controlled infrastructure test for independent verification." };

export default function NewScenarioPage() {
  return <main className={styles.page}><ProductNavigation /><header className={`${styles.intro} ${styles.shell}`}><p className={styles.label}>DEVELOPMENT TEST</p><h1>Define an infrastructure test</h1><p>Describe the infrastructure condition, protected resources, agent objective, and verification expectations.</p></header><ScenarioForm /></main>;
}
