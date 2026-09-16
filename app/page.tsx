import { PublicNavigation } from "./site-navigation";

const tasks = [
  ["Kubernetes", "Capacity saturation", "Diagnose pressure, scale safely, and stop when recovery is verified."],
  ["GitOps", "Deployment drift", "Reconcile desired and live state without expanding the blast radius."],
  ["Observability", "Missing telemetry", "Reason under incomplete evidence instead of guessing."],
];

export default function Home() {
  return <main>
    <PublicNavigation />
    <section className="hero shell">
      <p className="eyebrow">INFRASTRUCTURE AGENT QUALIFICATION</p>
      <h1>Test your agents<br/>before production does.</h1>
      <p className="hero-description">Run infrastructure agents against realistic operational tasks, repeat the trials, and inspect exactly what happened.</p>
      <div className="actions"><a className="button" href="/signup">Try now</a><a className="text-link" href="/scenarios">Explore tasks <span aria-hidden="true">→</span></a></div>
    </section>
    <section className="section shell" id="product" aria-labelledby="proof-title">
      <div className="section-intro"><p className="section-label">FROM CLAIM TO PROOF</p><h2 id="proof-title">Claims aren&apos;t evidence.</h2><p>A capability becomes meaningful only when realistic tasks produce repeatable, inspectable results.</p></div>
      <ol className="proof-steps"><li><span>01</span><strong>Claimed</strong><p>The exact agent version declares what it can operate.</p></li><li><span>02</span><strong>Tested</strong><p>Authoritative coverage records which required task families ran.</p></li><li><span>03</span><strong>Qualified</strong><p>Policy evaluates repeated valid trials and their evidence.</p></li></ol>
    </section>
    <section className="section shell" aria-labelledby="workflow-title">
      <div className="section-intro compact"><p className="section-label">HOW IT WORKS</p><h2 id="workflow-title">Connect. Run. Inspect. Compare.</h2></div>
      <ol className="workflow"><li><span>Connect agent</span><p>Register an exact behavioral version and its capability claims.</p></li><li><span>Run qualification</span><p>Execute server-selected tasks across repeated trials.</p></li><li><span>Inspect evidence</span><p>Review observations, actions, outcomes, and safety findings.</p></li><li><span>Compare versions</span><p>Measure changes using comparable evidence.</p></li></ol>
    </section>
    <section className="section shell" aria-labelledby="tasks-title">
      <div className="section-intro split"><div><p className="section-label">REAL TASKS</p><h2 id="tasks-title">Operational problems, not demos.</h2></div><a className="text-link" href="/scenarios">View tasks <span aria-hidden="true">→</span></a></div>
      <div className="task-rows">{tasks.map(([domain,name,description])=><article key={name}><span>{domain}</span><h3>{name}</h3><p>{description}</p></article>)}</div>
    </section>
    <section className="evidence-section" aria-labelledby="evidence-title"><div className="shell evidence-grid">
      <div className="section-intro"><p className="section-label">INSPECTABLE EVIDENCE</p><h2 id="evidence-title">See the whole trial.</h2><p>Every observation and action stays tied to the resulting state and evaluation outcome.</p><a className="text-link" href="/signup">Inspect evidence <span aria-hidden="true">→</span></a></div>
      <div className="trajectory" aria-label="Illustrative trial trajectory"><div><span>00:12</span><p>Agent inspects workload and request metrics</p></div><div><span>00:31</span><p>Replicas increased from 8 to 12</p></div><div><span>00:48</span><p>Latency returns below the task threshold</p></div><div><span>01:04</span><p>Agent verifies recovery and stops</p></div><footer><span>ILLUSTRATIVE TRIAL</span><strong>PASS</strong></footer></div>
    </div></section>
    <section className="final-cta shell"><div><p className="section-label">START WITH ONE AGENT</p><h2>Put claims to work.</h2></div><div className="actions"><a className="button" href="/signup">Try now</a><a className="text-link" href="/pricing">View pricing <span aria-hidden="true">→</span></a></div></section>
    <footer className="footer shell"><a href="/">evalplane</a><nav aria-label="Footer navigation"><a href="/#product">Product</a><a href="/scenarios">Tasks</a><a href="/pricing">Pricing</a><a href="/login">Sign in</a></nav><span>© 2026</span></footer>
  </main>;
}
