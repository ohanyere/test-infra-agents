import { PublicNavigation } from "./site-navigation";

const tasks = [
  ["Kubernetes", "Capacity saturation", "Diagnose pressure, scale safely, and stop when recovery is verified."],
  ["Kubernetes", "Deployment failure", "Restore a failed rollout without expanding the blast radius."],
  ["Kubernetes", "Dependency outage", "Distinguish a dependency failure from a workload fault."],
  ["GitOps", "Source-of-truth drift", "Reconcile desired and live state through the owned control path."],
  ["Delivery", "Canary degradation", "Respond to a degrading rollout while healthy cases reward restraint."],
  ["Safety", "Protected-resource boundaries", "Respect denied operations and task-owned scope."],
];

export default function Home() {
  return <main>
    <PublicNavigation />
    <section className="hero shell">
      <p className="eyebrow">INFRASTRUCTURE AGENT QUALIFICATION</p>
      <h1>Prove what your infrastructure agents can operate.</h1>
      <p className="hero-description">Qualify their capabilities against controlled infrastructure failures, inspect what they actually do, and build evidence before production access.</p>
      <div className="actions"><a className="button" href="/signup">Try now</a><a className="text-link" href="/scenarios">View tasks <span aria-hidden="true">→</span></a></div>
    </section>
    <section className="section shell" id="product" aria-labelledby="proof-title">
      <div className="section-intro"><p className="section-label">FROM CLAIM TO PROOF</p><h2 id="proof-title">From capability claim to operational proof.</h2></div>
      <ol className="proof-steps"><li><span>01</span><strong>Operational conditions</strong><p>Agents face controlled infrastructure problems, not only prompts or response grading.</p></li><li><span>02</span><strong>Bounded actions</strong><p>Evalplane limits what an agent may inspect or change and records requested versus executed actions.</p></li><li><span>03</span><strong>Qualification evidence</strong><p>Outcome, safety, coverage, reliability, and version regression inform the qualification decision.</p></li></ol>
    </section>
    <section className="section shell" aria-labelledby="status-title">
      <div className="section-intro"><p className="section-label">QUALIFICATION STATUS</p><h2 id="status-title">Claims aren&apos;t evidence.</h2></div>
      <ol className="proof-steps"><li><span>01</span><strong>Claimed</strong><p>This AgentVersion declares the capability.</p></li><li><span>02</span><strong>Tested</strong><p>Required independent capability coverage has been exercised under the server coverage contract.</p></li><li><span>03</span><strong>Qualified</strong><p>Evidence satisfies the qualification policy.</p></li></ol>
    </section>
    <section className="section shell" aria-labelledby="workflow-title">
      <div className="section-intro compact"><p className="section-label">HOW IT WORKS</p><h2 id="workflow-title">Connect. Run. Inspect. Compare.</h2></div>
      <ol className="workflow"><li><span>Connect agent</span><p>Register an exact AgentVersion and its capability claims.</p></li><li><span>Run qualification</span><p>Exercise the server-selected operational coverage.</p></li><li><span>Inspect evidence</span><p>Review observations, actions, outcomes, and safety findings.</p></li><li><span>Compare versions</span><p>Measure consistency and regression across exact versions.</p></li></ol>
    </section>
    <section className="section shell" aria-labelledby="tasks-title">
      <div className="section-intro split"><div><p className="section-label">REAL TASKS</p><h2 id="tasks-title">Operational problems, not demos.</h2></div><a className="text-link" href="/scenarios">View tasks <span aria-hidden="true">→</span></a></div>
      <div className="task-rows">{tasks.map(([domain,name,description])=><article key={name}><span>{domain}</span><h3>{name}</h3><p>{description}</p></article>)}</div>
    </section>
    <section className="evidence-section" aria-labelledby="evidence-title"><div className="shell evidence-grid">
      <div className="section-intro"><p className="section-label">INSPECTABLE EVIDENCE</p><h2 id="evidence-title">Evidence, not a score.</h2><p>Inspect what the agent observed and requested, what was authorized and executed, and the resulting system outcome.</p><a className="text-link" href="/signup">Inspect evidence <span aria-hidden="true">→</span></a></div>
      <div className="trajectory" aria-label="Illustrative trial trajectory"><div><span>OBSERVED</span><p>Workload saturation and elevated request latency</p></div><div><span>REQUESTED</span><p>Increase task-owned workload replicas</p></div><div><span>EXECUTED</span><p>Authorized scale from 8 to 12 replicas</p></div><div><span>OUTCOME</span><p>Latency recovers and the agent stops</p></div><footer><span>ILLUSTRATIVE EVIDENCE</span><strong>PASS</strong></footer></div>
    </div></section>
    <section className="final-cta shell"><div><p className="section-label">START WITH ONE AGENT</p><h2>Find out what it can operate.</h2></div><div className="actions"><a className="button" href="/signup">Try now</a><a className="text-link" href="/pricing">View pricing <span aria-hidden="true">→</span></a></div></section>
    <footer className="footer shell"><a href="/">evalplane</a><nav aria-label="Footer navigation"><a href="/#product">Product</a><a href="/scenarios">Tasks</a><a href="/pricing">Pricing</a><a href="/login">Sign in</a></nav><span>© 2026</span></footer>
  </main>;
}
