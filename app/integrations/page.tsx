import { ProductNavigation } from "../site-navigation";
import styles from "../product.module.css";

const integrations = [
  { name: "External infrastructure agents", state: "IMPLEMENTED", detail: "Workspace-scoped HTTP Agent API registration, exact version identity, encrypted bearer credentials, connection probes, and MCP tool access. Identity provenance is declared, not cryptographically attested." },
  { name: "Kubernetes and Prometheus", state: "DEVELOPMENT ONLY", detail: "The active Kind provider can create run-owned namespace environments; the verifier captures workload state and supported telemetry. It is not a customer-cluster connector or production provider." },
  { name: "Customer Kubernetes observation", state: "IMPLEMENTED · OBSERVATION ONLY", detail: "A tenant-scoped customer-hosted collector can submit ordered Deployment/Pod snapshots for a persisted observation-only run. Credentials are hashed server-side; collection is namespace-scoped and read-only. The collector does not provide telemetry/GitOps evidence or agent action attribution, so its current canonical result remains INCONCLUSIVE when required evidence is absent." },
  { name: "Git desired state and Argo CD", state: "FIXTURE / QUALIFICATION", detail: "The verifier persists Git desired-state and Argo evidence when configured. Current hosted proof uses controlled fixtures; customer repository credentials and connection management are not available." },
  { name: "CI/CD triggering", state: "API BUILDING BLOCKS", detail: "Authenticated evaluation-batch and suite-execution APIs exist. There is no dedicated CI token, trigger endpoint, or required-check adapter today." },
  { name: "Webhooks and notifications", state: "NOT AVAILABLE", detail: "The only webhook route is an incoming billing-provider webhook. Customer verification-result delivery and outgoing notifications are not implemented." },
  { name: "SDK", state: "NOT AVAILABLE", detail: "No customer SDK is shipped. The versioned HTTP API is the supported integration surface; credentials are session-based and workspace-authorized." },
];

export default function IntegrationsPage() {
  return <><ProductNavigation active="integrations" /><main className={styles.page}>
    <header className={styles.header}><div><p className={styles.eyebrow}>INTEGRATION STATUS</p><h1>Connect the actor. Keep verification independent.</h1><p>This page reflects current repository capabilities and hosted qualification evidence. An adapter in a controlled fixture is not a customer production integration.</p></div><div className={styles.actions}><a href="/agents/new">Connect an agent</a></div></header>
    <section className={styles.section}>{integrations.map((integration) => <article className={styles.integrationRow} key={integration.name}><div><p className={styles.eyebrow}>{integration.state}</p><h2>{integration.name}</h2></div><p>{integration.detail}</p><span>{integration.state}</span></article>)}</section>
    <section className={styles.section}><h2>Current customer workflow</h2><ol><li>Create an account and workspace.</li><li>Register an external HTTP Agent API version and test its connection.</li><li>Register a staging or production Kubernetes observation scope, enroll its read-only collector, and create an observation-only run.</li><li>Install the collector with namespace-scoped read-only RBAC and inspect the canonical persisted result when its observation window completes.</li><li>Build a qualification plan and immutable suite for controlled supported cases.</li></ol><p className={styles.notice}>Customer Kubernetes collection is observation-only preview: no remediation is executed, and missing required evidence or action attribution stays INCONCLUSIVE. It is not production safety certification.</p></section>
  </main></>;
}
