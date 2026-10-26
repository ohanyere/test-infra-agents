import type { VerificationResult } from "../../../lib/api";
import styles from "../../product.module.css";

export function VerificationEvidenceSections({ verification }: { verification: VerificationResult }) {
  const provenance = verification.provenance;
  const nextStep = verification.overall_status === "VERIFIED"
    ? "The captured evidence supports the stated outcome. Continue through your normal change controls; realverax does not authorize production changes."
    : verification.overall_status === "FAILED"
      ? "Review failed invariants, unexpected mutations, and blocking side effects before deciding on a remediation."
      : verification.overall_status === "WARNING"
        ? "Review each forward-risk finding and its supporting evidence with the service owner before accepting the resulting state."
        : "Resolve the unavailable or contradictory evidence dimensions and run a new verification. This result is not a pass.";

  return <>
    <section className={styles.section}><h2>Evidence completeness</h2><p className={styles.muted}>Assessment contract: {verification.evidence_assessment.contract_version}. Required evidence is not substituted when unavailable.</p><div className={styles.evidenceRows}>{verification.evidence_assessment.dimensions.map((dimension) => <div key={dimension.evidence_type}><strong>{dimension.evidence_type.replaceAll("_", " ")}</strong><span>{dimension.availability} · {dimension.requirement_status}</span><span>{dimension.source_provider}</span><span>{dimension.reason}</span>{dimension.evidence_references.map((reference) => <code key={reference}>{reference}</code>)}</div>)}</div></section>
    <section className={styles.section}><h2>Forward-risk findings</h2>{verification.forward_risks.length === 0 ? <p className={styles.muted}>No forward-risk finding was produced from the available evidence. This does not imply risks outside the observed scope were ruled out.</p> : <div className={styles.evidenceRows}>{verification.forward_risks.map((risk, index) => <div key={`${risk.analyzer_id}-${risk.type}-${index}`}><strong>{risk.severity.toUpperCase()} · {risk.type}</strong><span>{risk.reason}</span><span>Trigger: {risk.trigger}</span><span>Affected resources: {risk.affected_resources.join(", ") || "Not specified"}</span><span>Evidence strength: {risk.evidence_strength} · Analyzer {risk.analyzer_id} {risk.analyzer_version}</span>{risk.evidence.map((item) => <code key={item}>{item}</code>)}</div>)}</div>}</section>
    <section className={styles.section}><h2>Agent identity and verification provenance</h2>{provenance ? <><p><strong>{provenance.agent_implementation.kind}</strong> · {provenance.agent_implementation.identifier} · {provenance.agent_implementation.provenance_level} identity</p><p className={styles.muted}>Declared identity is not a cryptographic attestation.</p><div className={styles.evidenceRows}>{Object.entries(provenance.contracts).map(([name, contract]) => <div key={name}><strong>{name.replaceAll("_", " ")}</strong><code>{contract}</code></div>)}{provenance.providers.map((provider) => <div key={`${provider.role}-${provider.provider_id}`}><strong>{provider.role}</strong><span>{provider.provider_id}</span><code>{provider.contract_version || "contract version not recorded"}</code></div>)}</div></> : <p className={styles.notice}>Verification provenance was not persisted for this result. No provenance is inferred.</p>}</section>
    <section className={styles.section}><h2>Operator next step</h2><p>{nextStep}</p><p className={styles.muted}>Recommendations are informational. realverax does not automatically reverse or mutate infrastructure.</p></section>
  </>;
}
