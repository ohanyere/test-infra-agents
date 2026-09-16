import type { Metadata } from "next";
import styles from "./pricing.module.css";

export const metadata: Metadata = {
  title: "Pricing — Evalplane",
  description: "Qualification plans and evaluation credits for infrastructure agents.",
};

const plans = [
  {
    name: "Developer",
    price: "$0",
    cadence: "free",
    purpose: "Try Evalplane with a first agent.",
    cta: "Get started",
    href: "/signup",
    features: ["1 user", "1 workspace", "2 agents", "10 standard evaluation trials / month", "Shared Kubernetes sandbox", "HTTP + MCP invocation", "Built-in evaluation tasks", "Basic trajectory + outcome evidence", "7-day evidence retention"],
  },
  {
    name: "Starter",
    price: "$49",
    cadence: "/ month",
    purpose: "For independent infrastructure-agent developers.",
    cta: "Get started",
    href: "/signup",
    features: ["1 workspace", "3 users", "10 agents", "Unlimited agent versions", "100 evaluation credits", "Shared Kubernetes evaluations", "HTTP + MCP invocation", "Custom evaluation tasks", "Repeated trials", "Hidden task variants", "Regression testing across versions", "30-day evidence retention"],
  },
  {
    name: "Pro",
    price: "$299",
    cadence: "/ month",
    purpose: "For teams building and shipping infrastructure agents.",
    cta: "Get started",
    href: "/signup",
    features: ["Up to 10 users", "500 evaluation credits", "Private qualification suites", "Production-incident-derived tasks", "Qualification policies", "Agent and version comparisons", "PagerDuty + Datadog (planned)", "CI qualification (planned)", "Webhooks (planned)", "Higher trial concurrency", "Longer evidence retention"],
  },
  {
    name: "Organization",
    price: "$999",
    cadence: "/ month",
    purpose: "For infrastructure, platform, and AI teams adopting multiple agents.",
    cta: "Contact us",
    href: "/signup",
    features: ["Larger team allowance", "2,000 evaluation credits", "Multiple qualification programs", "Audit and governance features (planned)", "Advanced RBAC", "Longer evidence retention", "Higher trial concurrency", "Qualification reports", "Priority support"],
  },
  {
    name: "Enterprise",
    price: "Custom",
    cadence: "",
    purpose: "Contact us for dedicated qualification capacity.",
    cta: "Contact us",
    href: "/signup",
    features: ["Dedicated evaluation capacity", "Private networking", "Custom retention", "SSO and enterprise controls", "Dedicated environments", "Volume pricing"],
  },
];

export default function PricingPage() {
  return <main className={styles.page}>
    <header className={styles.siteHeader}><nav className={`${styles.nav} ${styles.navShell}`} aria-label="Main navigation">
      <a className={styles.wordmark} href="/" aria-label="Evalplane home">evalplane</a>
      <div className={styles.navLinks} aria-label="Primary navigation"><a href="/#how-qualification-works">Product</a><a href="/scenarios">Evaluation Tasks</a><a href="/#environments">Environments</a><a className={styles.activeNavLink} href="/pricing" aria-current="page">Pricing</a></div>
      <div className={styles.navActions}><a className={styles.signIn} href="/login">Sign in</a><a className={`${styles.button} ${styles.compactButton}`} href="/signup">Get started</a></div>
      <details className={styles.mobileNav}><summary aria-label="Open navigation"><span /><span /></summary><div className={styles.mobileNavPanel}><a href="/#how-qualification-works">Product</a><a href="/scenarios">Evaluation Tasks</a><a href="/#environments">Environments</a><a className={styles.mobileActiveLink} href="/pricing" aria-current="page">Pricing</a><a href="/login">Sign in</a><a className={styles.button} href="/signup">Get started</a></div></details>
    </nav></header>

    <header className={`${styles.intro} ${styles.shell}`}>
      <p className={styles.eyebrow}>PRICING</p>
      <h1>Pay for the infrastructure your trials require.</h1>
      <p>Qualify infrastructure agents before they reach production. Plans scale by evaluation credits, environments, and evidence retention — not by vague seat tiers.</p>
    </header>

    <section className={`${styles.plans} ${styles.shell}`} aria-label="Plans">
      {plans.map((plan) => <article key={plan.name} className={styles.plan}>
        <p className={styles.planName}>{plan.name}</p>
        <p className={styles.planPrice}>{plan.price}{plan.cadence && <span> {plan.cadence}</span>}</p>
        <p className={styles.planPurpose}>{plan.purpose}</p>
        <ul>{plan.features.map((feature) => <li key={feature}>{feature}</li>)}</ul>
        <a className={plan.name === "Starter" || plan.name === "Pro" ? styles.primaryAction : styles.secondaryAction} href={plan.href}>{plan.cta}</a>
      </article>)}
    </section>

    <section className={`${styles.credits} ${styles.shell}`} aria-labelledby="credits-title">
      <p className={styles.eyebrow}>EVALUATION CREDITS</p>
      <h2 id="credits-title">Evaluation credits account for the infrastructure required to execute trials.</h2>
      <p>A standard Kubernetes namespace trial is the base credit unit. Higher-fidelity environments — such as dedicated or cloud-attached capacity — consume more credits per trial. Exact multipliers for unreleased environments are not yet published.</p>
      <dl>
        <div><dt>Base unit</dt><dd>Standard Kubernetes namespace trial</dd></div>
        <div><dt>Higher fidelity</dt><dd>Consumes additional credits per trial</dd></div>
        <div><dt>Billing</dt><dd>Plans above include monthly credits; billing automation is not yet enabled</dd></div>
      </dl>
    </section>

    <section className={`${styles.availability} ${styles.shell}`} aria-labelledby="env-note-title">
      <p className={styles.eyebrow}>ENVIRONMENT AVAILABILITY</p>
      <h2 id="env-note-title">Priced environments match shipped environments.</h2>
      <p>Only the Kubernetes Sandbox is available today. GitOps, dedicated Kubernetes, and AWS sandboxes are planned or future and are not billed until they ship. Features marked “planned” above are not yet implemented.</p>
      <a className={styles.secondaryAction} href="/#environments">See environment status</a>
    </section>
  </main>;
}
