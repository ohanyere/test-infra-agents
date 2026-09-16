import type { Metadata } from "next";
import directoryStyles from "../scenarios.module.css";
import { ScenarioForm } from "./scenario-form";
import styles from "./new-scenario.module.css";

export const metadata: Metadata = { title: "Define an evaluation task — Evalplane", description: "Define a custom infrastructure evaluation task." };

function SiteHeader() {
  return <header className={directoryStyles.siteHeader}><nav className={`${directoryStyles.nav} ${directoryStyles.navShell}`} aria-label="Main navigation">
    <a className={directoryStyles.wordmark} href="/" aria-label="Evalplane home">evalplane</a>
    <div className={directoryStyles.navLinks} aria-label="Primary navigation"><a href="/#how-qualification-works">Product</a><a className={directoryStyles.activeNavLink} href="/scenarios" aria-current="page">Evaluation Tasks</a><a href="/#environments">Environments</a><a href="/pricing">Pricing</a></div>
    <div className={directoryStyles.navActions}><a className={directoryStyles.signIn} href="/login">Sign in</a><a className={`${directoryStyles.button} ${directoryStyles.compactButton}`} href="/signup">Get started</a></div>
    <details className={directoryStyles.mobileNav}><summary aria-label="Open navigation"><span/><span/></summary><div className={directoryStyles.mobileNavPanel}><a href="/#how-qualification-works">Product</a><a className={directoryStyles.mobileActiveLink} href="/scenarios" aria-current="page">Evaluation Tasks</a><a href="/#environments">Environments</a><a href="/pricing">Pricing</a><a href="/login">Sign in</a><a className={directoryStyles.button} href="/signup">Get started</a></div></details>
  </nav></header>;
}

export default function NewScenarioPage() {
  return <main className={styles.page}><SiteHeader /><header className={`${styles.intro} ${styles.shell}`}><p className={styles.label}>CUSTOM EVALUATION TASK</p><h1>Define an evaluation task.</h1><p>Define the conditions, expected behavior, and qualification criteria you want an infrastructure agent to prove it can handle across repeated trials.</p><p className={styles.supporting}>Use a real failure you have experienced, a workload you expect to face, or a condition you want an agent to prove it can recover from.</p></header><ScenarioForm /></main>;
}
