import { PublicNavigation } from "./site-navigation";
import { SiteFooter } from "./site-footer";

const verificationModel = [
  ["01", "BEFORE STATE", "What was true before the autonomous change?"],
  ["02", "INTENT", "What should the change accomplish?"],
  ["03", "ACTUAL CHANGE", "What infrastructure actually changed?"],
  ["04", "OUTCOME", "Did the intended operational result happen?"],
  ["05", "INVARIANTS", "What conditions must remain true?"],
  ["06", "COLLATERAL EFFECTS", "What else changed or degraded?"],
  ["07", "STABILITY", "Did the state hold after reconciliation?"],
  ["08", "RESIDUAL RISK", "What credible risk did the resulting state leave behind?"],
];
const tests = ["Failed rollout", "GitOps drift", "Unsafe scaling", "Dependency degradation", "Configuration regression", "Capacity pressure"];

export default function Home() {
  return <main>
    <PublicNavigation />
    <section className="hero shell">
      <p className="eyebrow">AUTONOMOUS INFRASTRUCTURE VERIFICATION</p>
      <h1>Verify autonomous<br/>infrastructure changes.</h1>
      <p className="hero-description">Independently prove that an automated infrastructure change worked, left the system stable, preserved critical invariants, and didn&apos;t introduce unacceptable risk.</p>
      <div className="actions"><a className="button" href="/signup">Start verifying</a><a className="secondary-button" href="#example-verification">See a verification</a></div>
    </section>
    <section className="proof-section shell" id="example-verification" aria-labelledby="demo-title">
      <article className="proof-report">
        <header className="proof-header"><div><p className="proof-kicker">DEMO VERIFICATION</p><h2 id="demo-title">Action succeeded. Resulting state failed.</h2><p className="proof-meta"><span>GitOps remediation</span><span>infra-agent v1.9.0</span></p></div><strong className="proof-status proof-failed">FAILED</strong></header>
        <div className="proof-objective"><span>OBJECTIVE</span><p>Recover checkout after failed deployment.</p></div>
        <section className="proof-block proof-action" aria-labelledby="action-inspection-title">
          <h3 id="action-inspection-title">AUTONOMOUS ACTION · SUCCESS</h3>
          <div className="proof-diff"><header><span>LIVE RESOURCE</span><strong>Deployment/checkout</strong></header><div><span>image</span><code>checkout:v18</code><b>→</b><code>checkout:v17</code></div></div>
        </section>
        <div className="proof-findings">
          <section className="proof-block"><h3>IMMEDIATE OUTCOME</h3><ul className="proof-checks"><li>Checkout recovered</li><li>Service health restored</li></ul></section>
          <section className="proof-block"><h3>INVARIANTS</h3><p className="proof-finding proof-failed">FAILED</p><p>Git desired state still declares v18. Live and desired state diverged.</p></section>
          <section className="proof-block"><h3>COLLATERAL EFFECT</h3><p className="proof-finding proof-failed">Argo CD is OutOfSync</p><p>The live fix did not update the source of truth.</p></section>
          <section className="proof-block"><h3>RESIDUAL RISK</h3><p className="proof-finding proof-warning">NOT DURABLE</p><p>Controller reconciliation can restore the bad version.</p></section>
        </div>
        <footer className="proof-result"><span>FINAL VERIFICATION · FAILED</span><p>A successful action is not the same thing as a safe resulting state.</p></footer>
      </article>
    </section>
    <section className="section shell proof-problem" id="product"><div className="section-intro"><p className="section-label">WHY VERIFY</p><h2>A successful API call doesn&apos;t mean the fix worked.</h2><p>Infrastructure keeps changing after an agent acts. Controllers reconcile. Dependencies absorb load. Desired and live state can diverge.<br/><br/>We inspect the resulting infrastructure, not just the agent&apos;s explanation.</p></div></section>
    <section className="section shell" id="verification-model" aria-labelledby="model-title"><div className="section-intro"><p className="section-label">VERIFICATION MODEL</p><h2 id="model-title">Follow the change through infrastructure reality.</h2></div><ol className="verification-model">{verificationModel.map(([number,label,copy])=><li key={label}><span>{number}</span><strong>{label}</strong><p>{copy}</p></li>)}</ol></section>
    <section className="section shell ci-section"><div className="section-intro"><p className="section-label">TWO MODES</p><h2>Verify before shipping. Then verify real changes.</h2><p><strong>Development:</strong> run autonomous systems against controlled infrastructure tests and inspect the states they leave behind.<br/><br/><strong>Production verification — planned:</strong> independently verify resulting state after real autonomous changes in the customer&apos;s own environment.</p></div></section>
    <section className="section shell boundary"><p className="section-label">PRODUCT BOUNDARY</p><h2>Your agent stays yours.</h2><div><p>We don&apos;t build the agent.</p><p>We don&apos;t provide its memory.</p><p>We don&apos;t decide whether it&apos;s intelligent.</p><strong>We verify what happens when it changes infrastructure.</strong></div></section>
    <section className="section shell"><div className="section-intro split"><div><p className="section-label">DEVELOPMENT TESTS</p><h2>Reproducible infrastructure situations.</h2><p>Tests create controlled autonomous changes so the resulting state can be independently verified before shipping an agent version.</p></div><a className="text-link" href="/scenarios">Explore test cases →</a></div><div className="scenario-rows">{tests.map((test,index)=><div key={test}><span>{String(index+1).padStart(2,"0")}</span><strong>{test}</strong></div>)}</div></section>
    <section className="final-cta shell"><div><p className="section-label">INDEPENDENT VERIFICATION</p><h2>Your agent says it&apos;s fixed.<br/>Verify it.</h2></div><a className="button" href="/signup">Start verifying</a></section>
    <SiteFooter />
  </main>;
}
