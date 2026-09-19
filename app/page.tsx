import { PublicNavigation } from "./site-navigation";

const verificationModel = [
  ["01", "BASELINE", "What was true before the agent acted?"],
  ["02", "INTENT", "What was the agent trying to accomplish?"],
  ["03", "ACTION INSPECTION", "What did it actually change?"],
  ["04", "OUTCOME", "Did the intended effect happen?"],
  ["05", "SIDE EFFECTS", "What else changed or degraded?"],
  ["06", "STABILITY", "Did the result remain healthy?"],
];
const scenarios = ["Failed rollout", "GitOps drift", "Unsafe scaling", "Dependency degradation", "Configuration regression", "Capacity pressure"];

export default function Home() {
  return <main>
    <PublicNavigation />
    <section className="hero shell">
      <p className="eyebrow">INFRASTRUCTURE AGENT VERIFICATION</p>
      <h1>Your agent changed infrastructure.<br/>Verify what happened next.</h1>
      <p className="hero-description">Inspect what your infrastructure agent actually changed. Verify the intended outcome, catch side effects, confirm stability, and surface forward risk — on every agent change.</p>
      <div className="actions"><a className="button" href="/signup">Start verifying</a><a className="secondary-button" href="#example-verification">See a verification</a></div>
    </section>
    <section className="proof-section shell" id="example-verification" aria-labelledby="demo-title">
      <article className="proof-report">
        <header className="proof-header"><div><p className="proof-kicker">DEMO VERIFICATION</p><h2 id="demo-title">GitOps remediation</h2><p className="proof-meta"><span>infra-agent v1.9.0</span><span>PR #248</span></p></div><strong className="proof-status proof-failed">FAILED</strong></header>
        <div className="proof-objective"><span>AGENT OBJECTIVE</span><p>Restore checkout after the failed rollout.</p></div>
        <section className="proof-block proof-action" aria-labelledby="action-inspection-title">
          <h3 id="action-inspection-title">ACTION INSPECTION</h3>
          <div className="proof-diff"><header><span>RESOURCE</span><strong>Deployment/checkout</strong></header><div><span>image</span><code>checkout:v18</code><b>→</b><code>checkout:v17</code></div><div><span>replicas</span><code>3</code><b>→</b><code>6</code></div></div>
        </section>
        <div className="proof-findings">
          <section className="proof-block"><h3>OUTCOME</h3><ul className="proof-checks"><li>Service recovered</li><li>6/6 replicas ready</li><li>Error rate recovered</li></ul></section>
          <section className="proof-block"><h3>SIDE EFFECT</h3><p className="proof-finding proof-warning">Argo CD became OutOfSync</p><p>Live Kubernetes state was changed without updating Git desired state.</p></section>
          <section className="proof-block"><h3>STABILITY</h3><p className="proof-finding proof-failed">FAILED</p><p>The fix is not durable.</p></section>
          <section className="proof-block"><h3>FORWARD RISK</h3><p className="proof-finding proof-warning">HIGH</p><p>Argo CD may reconcile the workload back to the broken desired state.</p></section>
        </div>
        <footer className="proof-result"><span>RESULT</span><p>The immediate incident recovered.<br/>The remediation will not hold.</p></footer>
      </article>
    </section>
    <section className="section shell proof-problem" id="product"><div className="section-intro"><p className="section-label">WHY VERIFY</p><h2>A successful API call doesn&apos;t mean the fix worked.</h2><p>Infrastructure keeps changing after an agent acts. Controllers reconcile. Dependencies absorb load. Desired and live state can diverge.<br/><br/>We inspect the resulting infrastructure, not just the agent&apos;s explanation.</p></div></section>
    <section className="section shell" aria-labelledby="model-title"><div className="section-intro"><p className="section-label">VERIFICATION MODEL</p><h2 id="model-title">Follow the change through the system.</h2></div><ol className="verification-model">{verificationModel.map(([number,label,copy])=><li key={label}><span>{number}</span><strong>{label}</strong><p>{copy}</p></li>)}</ol><div className="forward-risk"><span>FORWARD RISK</span><div><h3>What credible problems did the new state introduce?</h3><p>Forward Risk is evidence-backed change-impact analysis, not certainty about future events.</p></div></div></section>
    <section className="section shell ci-section"><div className="section-intro"><p className="section-label">CONTINUOUS VERIFICATION</p><h2>Run it before every merge.</h2><p>Change the model. Change the prompt. Add a tool. Rewrite the agent.<br/>Your normal CI runs. So do we.</p></div><div className="ci-grid"><article className="check-run"><header><div><span>PR #248</span><strong>Improve GitOps remediation</strong></div><small>DEMO CHECK</small></header><ul><li><span>✓</span> Build</li><li><span>✓</span> Unit tests</li><li><span>✓</span> Security</li><li className="failed"><span>✕</span> Infrastructure Agent Verification</li></ul><footer><span>12 scenarios · 10 verified · 1 warning · 1 failed</span><a href="#example-verification">View evidence →</a></footer></article><article className="version-compare"><header><span>VERSION COMPARISON · DEMO DATA</span><strong>v1.8.4 → v1.9.0</strong></header><dl><div><dt>Verified scenarios</dt><dd>10 → 11</dd></div><div><dt>Unexpected mutations</dt><dd>3 → 1</dd></div><div><dt>Side effects</dt><dd>2 → 1</dd></div><div><dt>Stability failures</dt><dd className="result-failed">0 → 1</dd></div></dl></article></div></section>
    <section className="section shell boundary"><p className="section-label">PRODUCT BOUNDARY</p><h2>Your agent stays yours.</h2><div><p>We don&apos;t build the agent.</p><p>We don&apos;t provide its memory.</p><p>We don&apos;t decide whether it&apos;s intelligent.</p><strong>We verify what happens when it changes infrastructure.</strong></div></section>
    <section className="section shell"><div className="section-intro split"><div><p className="section-label">SCENARIOS</p><h2>Real infrastructure situations.</h2><p>Run your agent against controlled infrastructure conditions before trusting the same behavior elsewhere.</p></div><a className="text-link" href="/scenarios">Explore scenarios →</a></div><div className="scenario-rows">{scenarios.map((scenario,index)=><div key={scenario}><span>{String(index+1).padStart(2,"0")}</span><strong>{scenario}</strong></div>)}</div></section>
    <section className="final-cta shell"><div><p className="section-label">INDEPENDENT VERIFICATION</p><h2>Your agent says it&apos;s fixed.<br/>Verify it.</h2></div><a className="button" href="/signup">Start verifying</a></section>
    <footer className="footer shell"><a href="/">evalplane</a><nav aria-label="Footer navigation"><a href="/#product">Product</a><a href="/scenarios">Scenarios</a><a href="/#docs">Docs</a><a href="/login">Sign in</a></nav><span>© 2026</span></footer>
  </main>;
}
