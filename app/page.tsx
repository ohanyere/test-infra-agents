import { PublicNavigation } from "./site-navigation";
import { SiteFooter } from "./site-footer";

const evidenceSteps = [
  ["01", "Intent", "The operational outcome and measurable criteria."],
  ["02", "Actions", "What was requested, allowed, denied, and executed."],
  ["03", "State change", "The infrastructure delta observed independently."],
  ["04", "Durability", "Recovery, invariants, stability, and forward risk."],
];

const conclusions = [
  { status: "VERIFIED", tone: "verified", title: "Recovered and stable", detail: "The objective passed and required evidence supports the resulting state." },
  { status: "FAILED", tone: "failed", title: "Outcome or safety failed", detail: "The objective failed, a required invariant broke, or a blocking side effect was found." },
  { status: "WARNING", tone: "warning", title: "Passed with a material risk", detail: "The objective passed, but evidence-backed forward risk still needs operator review." },
  { status: "INCONCLUSIVE", tone: "inconclusive", title: "Evidence was not sufficient", detail: "A missing or contradictory evidence channel prevents a trustworthy conclusion." },
];

export default function Home() {
  return <main>
    <PublicNavigation />
    <section className="hero shell">
      <p className="eyebrow">INDEPENDENT INFRASTRUCTURE VERIFICATION</p>
      <h1>Your agent says<br />it worked. Prove it.</h1>
      <p className="hero-description">realverax independently checks the infrastructure an autonomous change leaves behind: what changed, whether the objective held, whether the change is attributable, and what risk remains.</p>
      <div className="actions"><a className="button" href="/signup">Create a workspace</a><a className="secondary-button" href="#verification-model">See how verification works</a></div>
      <p className="hero-footnote">Built for infrastructure and SRE agents. Current execution environments are development-only; production connections are not yet available.</p>
    </section>

    <section className="proof-section shell" id="example-verification" aria-labelledby="example-title">
      <article className="proof-report">
        <header className="proof-header"><div><p className="proof-kicker">ILLUSTRATIVE VERIFICATION · NOT CUSTOMER DATA</p><h2 id="example-title">An action can succeed while the fix is unsafe.</h2><p className="proof-meta"><span>GitOps remediation</span><span>Example state only</span></p></div><strong className="proof-status proof-failed">FAILED</strong></header>
        <div className="proof-objective"><span>INTENDED OUTCOME</span><p>Restore checkout service after a failed deployment.</p></div>
        <section className="proof-block proof-action" aria-labelledby="example-action-title">
          <h3 id="example-action-title">OBSERVED CHANGE</h3>
          <div className="proof-diff"><header><span>LIVE DEPLOYMENT</span><strong>checkout / image</strong></header><div><span>image</span><code>checkout:v18</code><b>→</b><code>checkout:v17</code></div></div>
        </section>
        <div className="proof-findings">
          <section className="proof-block"><h3>OPERATIONAL OUTCOME</h3><ul className="proof-checks"><li>Service health recovered</li><li>Recovery objective passed</li></ul></section>
          <section className="proof-block"><h3>DESIRED / LIVE STATE</h3><p className="proof-finding proof-failed">DIVERGED</p><p>Git still declares v18 while the live Deployment runs v17.</p></section>
          <section className="proof-block"><h3>CONTROLLER STATE</h3><p className="proof-finding proof-failed">OUT OF SYNC</p><p>Reconciliation may restore the version that caused the incident.</p></section>
          <section className="proof-block"><h3>EVIDENCE BASIS</h3><p className="proof-finding">STATE + GIT + ARGO</p><p>The agent&apos;s success claim is not used as proof of infrastructure outcome.</p></section>
        </div>
        <footer className="proof-result"><span>EXAMPLE CONCLUSION</span><p>Recovery happened. The resulting change was not durable.</p></footer>
      </article>
    </section>

    <section className="section shell" id="product"><div className="section-intro"><p className="section-label">THE VERIFICATION GAP</p><h2>A successful tool call is not an operational result.</h2><p>Controllers reconcile. Dependencies absorb load. Desired and live state can diverge. realverax compares independent infrastructure evidence with the intended outcome—without trusting the agent to grade itself.</p></div></section>

    <section className="section shell" id="verification-model" aria-labelledby="model-title"><div className="section-intro"><p className="section-label">A TRACEABLE CONCLUSION</p><h2 id="model-title">Follow the change through infrastructure reality.</h2><p>Each conclusion is tied to persisted evidence, its source, and the time it was observed.</p></div><ol className="verification-model">{evidenceSteps.map(([number,label,copy])=><li key={label}><span>{number}</span><strong>{label.toUpperCase()}</strong><p>{copy}</p></li>)}</ol></section>

    <section className="section shell" aria-labelledby="conclusions-title"><div className="section-intro"><p className="section-label">FOUR POSSIBLE CONCLUSIONS</p><h2 id="conclusions-title">The result says more than “pass” or “fail.”</h2><p>These are product semantics, not a summary of customer activity. No customer runs are shown on this public page.</p></div><div className="conclusion-grid">{conclusions.map((item)=><article className={`conclusion-card ${item.tone}`} key={item.status}><p>{item.status}</p><h3>{item.title}</h3><span>{item.detail}</span></article>)}</div></section>

    <section className="section shell" id="integrations"><div className="section-intro"><p className="section-label">INTEGRATION BOUNDARY</p><h2>Connect the agent. Verify against independent evidence.</h2><p>Agent versions connect over the HTTP Agent API or MCP access path. A customer-hosted read-only Kubernetes collector can now submit scoped observations for the canonical report; without action attribution, telemetry, or other required evidence, the result remains INCONCLUSIVE. Controlled execution and Prometheus/GitOps customer connections are not implied.</p><div className="actions"><a className="secondary-button" href="/integrations">See current integration status</a><a className="text-link" href="/agents/new">Connect an agent →</a></div></div></section>

    <section className="section shell boundary"><p className="section-label">THE PRODUCT BOUNDARY</p><h2>Your agent stays yours.</h2><div><p>We do not build or orchestrate the agent.</p><p>We do not treat its trace or success message as ground truth.</p><strong>We verify the infrastructure state it leaves behind.</strong></div></section>

    <section className="section shell"><div className="section-intro split"><div><p className="section-label">START WITH A CONTROLLED RUN</p><h2>Register an exact version. Run a bounded verification.</h2><p>Connect an agent, build a qualification suite from supported test cases, review the persisted result, and inspect its evidence timeline.</p></div><a className="text-link" href="/signup">Create a workspace →</a></div><div className="scenario-rows">{evidenceSteps.map(([number,label,copy])=><div key={number}><span>{number}</span><strong>{label} · {copy}</strong></div>)}</div></section>

    <section className="final-cta shell"><div><p className="section-label">INDEPENDENT VERIFICATION</p><h2>Your agent says it worked.<br />Prove it.</h2></div><a className="button" href="/signup">Create a workspace</a></section>
    <SiteFooter />
  </main>;
}
