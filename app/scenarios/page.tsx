import type { Metadata } from "next";
import { ScenarioDirectory } from "./scenario-directory";
import styles from "./scenarios.module.css";

export const metadata: Metadata = { title: "Evaluation Tasks — Evalplane", description: "Browse evaluation tasks grouped by task family for qualifying infrastructure agents." };

export default function ScenariosPage() {
  return <main className={styles.page}>
    <header className={styles.siteHeader}><nav className={`${styles.nav} ${styles.navShell}`} aria-label="Main navigation">
      <a className={styles.wordmark} href="/" aria-label="Evalplane home">evalplane</a>
      <div className={styles.navLinks} aria-label="Primary navigation"><a href="/#how-qualification-works">Product</a><a className={styles.activeNavLink} href="/scenarios" aria-current="page">Evaluation Tasks</a><a href="/#environments">Environments</a><a href="/pricing">Pricing</a></div>
      <div className={styles.navActions}><a className={styles.signIn} href="/login">Sign in</a><a className={`${styles.button} ${styles.compactButton}`} href="/signup">Get started</a></div>
      <details className={styles.mobileNav}><summary aria-label="Open navigation"><span/><span/></summary><div className={styles.mobileNavPanel}><a href="/#how-qualification-works">Product</a><a className={styles.mobileActiveLink} href="/scenarios" aria-current="page">Evaluation Tasks</a><a href="/#environments">Environments</a><a href="/pricing">Pricing</a><a href="/login">Sign in</a><a className={styles.button} href="/signup">Get started</a></div></details>
    </nav></header>
    <header className={`${styles.intro} ${styles.shell}`}><p className={styles.sectionLabel}>EVALUATION TASKS</p><h1>Tasks that test what an agent claims to operate.</h1><p>Each evaluation task runs an infrastructure agent through realistic conditions in an isolated environment, across repeated trials. Tasks are grouped by task family so a qualification suite maps directly to capability claims.</p><a className={styles.primaryAction} href="/scenarios/new">Define a custom task</a></header>
    <ScenarioDirectory />
  </main>;
}
