import type { Metadata } from "next";
import { AgentDirectory } from "./agent-directory";
import styles from "./agents.module.css";

export const metadata: Metadata = {
  title: "Connect an Agent — Evalplane",
  description: "Connect an infrastructure agent and qualify the capabilities it claims to support.",
};

export default function AgentsPage() {
  return (
    <main className={styles.page}>
      <header className={styles.siteHeader}>
        <nav className={`${styles.nav} ${styles.shell}`} aria-label="Main navigation">
          <a className={styles.wordmark} href="/workspace" aria-label="Evalplane workspace">
            evalplane
          </a>
          <div className={styles.navLinks} aria-label="Primary navigation">
            <a href="/workspace">Overview</a>
            <a href="/qualification">Qualification</a>
            <a href="/scenarios">Tasks</a>
            <a href="/runs">Activity</a>
          </div>
          <div className={styles.navActions}>
            <a className={`${styles.button} ${styles.compactButton}`} href="/workspace/billing">Billing</a>
          </div>
          <details className={styles.mobileNav}>
            <summary aria-label="Open navigation"><span /><span /></summary>
            <div className={styles.mobileNavPanel}>
              <a href="/workspace">Overview</a><a href="/qualification">Qualification</a><a href="/scenarios">Tasks</a><a href="/runs">Activity</a><a href="/workspace/billing">Billing</a>
            </div>
          </details>
        </nav>
      </header>

      <section className={`${styles.intro} ${styles.shell}`} aria-labelledby="agents-title">
        <p className={styles.eyebrow}>INFRASTRUCTURE AGENTS</p>
        <h1 id="agents-title">Connect an infrastructure agent.</h1>
        <p className={styles.description}>
          Connect an infrastructure agent and qualify the capabilities it claims to support.
          Each registered version records its HTTP invocation, direct or MCP tool access,
          endpoint, connection status, and declared capability claims — the basis for
          claimed, tested, and qualification status.
        </p>
      </section>

      <AgentDirectory />
    </main>
  );
}
