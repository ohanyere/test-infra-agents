import { ProductNavigation } from "../site-navigation";
import styles from "../product.module.css";

const integrations = [
  { name: "Kubernetes", status: "AVAILABLE", copy: "Scoped workload state, ownership, actions, and before/after observations in the current development runtime." },
  { name: "Prometheus", status: "AVAILABLE", copy: "Scenario-scoped service telemetry for deterministic outcome and bounded stability verification." },
  { name: "GitHub / CI", status: "IN DEVELOPMENT", copy: "An internal hosted E2E workflow exists. Customer repository authentication and required-check integration are not available." },
  { name: "Argo CD / Git", status: "IN DEVELOPMENT", copy: "A narrow pinned-Git reader and read-only Argo evidence adapter support the hosted proof. Customer repository connections are not available." },
  { name: "Terraform", status: "PLANNED", copy: "No Terraform state collector or verification adapter is implemented." },
  { name: "Cloud audit and state", status: "PLANNED", copy: "No AWS, Azure, or GCP production integration is implemented." },
];

export default function IntegrationsPage() {
  return <><ProductNavigation active="integrations" /><main className={styles.page}>
    <header className={styles.header}><div><p className={styles.eyebrow}>INFRASTRUCTURE EVIDENCE</p><h1>Integrations</h1><p>Independent verification depends on infrastructure-native evidence. Availability labels describe current repository support, not roadmap promises.</p></div></header>
    <section className={styles.section}>{integrations.map((integration) => <div className={styles.integrationRow} key={integration.name}><div><p className={styles.eyebrow}>{integration.status}</p><h2>{integration.name}</h2><p>{integration.copy}</p></div><div><p>{integration.status === "AVAILABLE" ? "Current development verification runtime" : integration.status === "IN DEVELOPMENT" ? "Not customer-operable yet" : "No implemented product integration"}</p></div><span className={integration.status === "AVAILABLE" ? styles.verifiedStatus : styles.inconclusiveStatus}>{integration.status}</span></div>)}</section>
    <p className={styles.demoNotice}>Production verification in a customer environment is planned. This page does not create collectors, cloud access, repository connections, or credentials.</p>
  </main></>;
}
