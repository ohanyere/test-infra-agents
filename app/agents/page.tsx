import type { Metadata } from "next";
import { AgentDirectory } from "./agent-directory";
import { ProductNavigation } from "../site-navigation";
import styles from "./agents.module.css";

export const metadata: Metadata = {
  title: "Connect an Agent — Evalplane",
  description: "Connect an infrastructure agent and qualify the capabilities it claims to support.",
};

export default function AgentsPage() {
  return (
    <main className={styles.page}>
      <ProductNavigation active="agents" />

      <section className={`${styles.intro} ${styles.shell}`} aria-labelledby="agents-title">
        <p className={styles.eyebrow}>AGENTS</p>
        <h1 id="agents-title">Agents</h1>
        <p className={styles.description}>Connect an agent and prove what it can operate.</p>
      </section>

      <AgentDirectory />
    </main>
  );
}
