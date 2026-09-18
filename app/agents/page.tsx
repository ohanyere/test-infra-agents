import type { Metadata } from "next";
import { AgentDirectory } from "./agent-directory";
import { ProductNavigation } from "../site-navigation";
import styles from "./agents.module.css";

export const metadata: Metadata = {
  title: "Infrastructure Agents — Evalplane",
  description: "Connect exact infrastructure agent versions and inspect their verification evidence.",
};

export default function AgentsPage() {
  return (
    <main className={styles.page}>
      <ProductNavigation active="agents" />

      <section className={`${styles.intro} ${styles.shell}`} aria-labelledby="agents-title">
        <p className={styles.eyebrow}>AGENTS</p>
        <h1 id="agents-title">Agents</h1>
        <p className={styles.description}>Connect exact versions and verify what happens when they change infrastructure.</p>
      </section>

      <AgentDirectory />
    </main>
  );
}
