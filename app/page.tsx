export default function Home() {
  return (
    <main>
      <header className="site-header">
        <nav className="nav shell" aria-label="Main navigation">
          <a className="wordmark" href="/" aria-label="Evalplane home">evalplane</a>
          <div className="nav-links" aria-label="Primary navigation">
            <a href="/#how-qualification-works">Product</a>
            <a href="/scenarios">Evaluation Tasks</a>
            <a href="/#environments">Environments</a>
            <a href="/pricing">Pricing</a>
          </div>
          <div className="nav-actions">
            <a className="sign-in" href="/login">Sign in</a>
            <a className="button button-compact" href="/signup">Get started</a>
          </div>
          <details className="mobile-nav">
            <summary aria-label="Open navigation"><span /><span /></summary>
            <div className="mobile-nav-panel">
              <a href="/#how-qualification-works">Product</a>
              <a href="/scenarios">Evaluation Tasks</a>
              <a href="/#environments">Environments</a>
              <a href="/pricing">Pricing</a>
              <a href="/login">Sign in</a>
              <a className="button" href="/signup">Get started</a>
            </div>
          </details>
        </nav>
      </header>

      <section className="hero shell">
        <div className="eyebrow">AN EVALUATION HARNESS AND QUALIFICATION PLATFORM FOR INFRASTRUCTURE AGENTS</div>
        <h1>Qualify infrastructure agents before they reach production.</h1>
        <p className="hero-description">
          Connect an infrastructure agent, test its claimed capabilities against realistic
          infrastructure tasks, and get reproducible evidence of what it can actually be
          trusted to operate.
        </p>
        <div className="hero-actions">
          <a className="button" href="/agents/new">Connect an agent</a>
          <a className="button button-secondary" href="/#how-qualification-works">See how qualification works</a>
        </div>
      </section>

      <section className="product-decisions shell" id="how-qualification-works" aria-labelledby="flow-title">
        <div className="decisions-intro">
          <div className="section-label">HOW QUALIFICATION WORKS</div>
          <h2 id="flow-title">Turn capability claims into qualification evidence.</h2>
          <p className="flow-lead">
            Infrastructure agents make capability claims. Evalplane tests those claims using
            realistic infrastructure tasks, isolated environments, repeated trials, controlled
            tools, deterministic outcomes, and reproducible evidence.
          </p>
        </div>
        <ol className="qualification-flow">
          <li><span>01</span><div><strong>Agent</strong><p>The infrastructure agent under evaluation, pinned to an exact version.</p></div></li>
          <li><span>02</span><div><strong>Capability claims</strong><p>What the agent claims it can operate — for example, remediating GitOps drift or recovering saturated workloads.</p></div></li>
          <li><span>03</span><div><strong>Qualification suite</strong><p>The set of evaluation tasks selected to test those claims.</p></div></li>
          <li><span>04</span><div><strong>Infrastructure tasks</strong><p>Realistic tasks drawn from production-like conditions: traffic saturation, failed rollouts, dependency faults.</p></div></li>
          <li><span>05</span><div><strong>Isolated environment</strong><p>Each trial runs in infrastructure isolated from production, matched to what the capability requires.</p></div></li>
          <li><span>06</span><div><strong>Repeated trials</strong><p>The same task is executed multiple times so consistency — not a single favorable run — is measured.</p></div></li>
          <li><span>07</span><div><strong>Trajectory + outcome evidence</strong><p>Every observation, action, and resulting system state is captured as inspectable evidence.</p></div></li>
          <li><span>08</span><div><strong>Qualified / Not qualified / Inconclusive</strong><p>The qualification policy renders a decision with reproducible evidence attached.</p></div></li>
        </ol>
      </section>

      <section className="claimed shell" aria-labelledby="claimed-title">
        <div className="decisions-intro">
          <div className="section-label">CLAIMED / TESTED / QUALIFIED</div>
          <h2 id="claimed-title">A claim is not a qualification.</h2>
          <p className="flow-lead">
            Declaring a capability does not make an agent qualified. Qualification is earned
            when claimed behavior holds up under repeated, realistic execution.
          </p>
        </div>
        <div className="claimed-grid">
          <article>
            <span>CLAIMED</span>
            <h3>&ldquo;This agent can remediate GitOps drift.&rdquo;</h3>
            <p>A statement made by the agent vendor or developer. It describes intent, not proof.</p>
          </article>
          <article>
            <span>TESTED</span>
            <h3>Put through realistic conditions.</h3>
            <ul>
              <li>Multiple task families</li>
              <li>Repeated trials</li>
              <li>Hidden variants</li>
              <li>Real infrastructure state</li>
            </ul>
          </article>
          <article>
            <span>QUALIFIED</span>
            <h3>Evidence that the claim held.</h3>
            <ul>
              <li>Requirements satisfied</li>
              <li>No forbidden safety behavior</li>
              <li>Reproducible evidence</li>
            </ul>
          </article>
        </div>
      </section>

      <section className="evaluation" id="evidence" aria-labelledby="evaluation-title">
        <div className="evaluation-inner shell">
          <div className="evaluation-intro">
            <div className="section-label">MEASURABLE EVIDENCE</div>
            <h2 id="evaluation-title">Trajectory and outcome, captured per trial.</h2>
            <p className="evaluation-description">
              An agent should not be approved because a demo looked convincing. Evalplane runs
              agents through realistic operational conditions, records the trajectory of what
              they observe and do, and evaluates the resulting outcome against measurable criteria.
            </p>
            <p className="example-label">ILLUSTRATIVE EXAMPLE — REFERENCE TRAJECTORY FORMAT, NOT A LIVE QUALIFICATION RESULT</p>
          </div>

          <div className="scenario" aria-label="Traffic spike evaluation trial illustration">
            <div className="scenario-header">
              <h3>Traffic Spike / Capacity Saturation</h3>
              <span>ILLUSTRATIVE EXAMPLE</span>
            </div>

            <div className="traffic-progression">
              <span>Traffic progression</span>
              <strong>1,000 req/s <span aria-hidden="true">→</span> 10,000 req/s</strong>
            </div>

            <ol className="scenario-timeline">
              <li><time>02:14</time><span>checkout-api CPU reaches 94%</span></li>
              <li><time>02:21</time><span>Agent inspects workload metrics</span></li>
              <li><time>02:34</time><span>Agent scales replicas 8 <span aria-hidden="true">→</span> 16</span></li>
              <li><time>02:52</time><span>Database connections reach 81%</span></li>
              <li><time>03:04</time><span>Agent stops further scaling</span></li>
              <li><time>03:31</time><span>Application SLO restored</span></li>
            </ol>

            <div className="scenario-result">
              <span>Trial outcome</span>
              <strong>PASS (ILLUSTRATIVE TRIAL)</strong>
            </div>
          </div>
        </div>
      </section>

      <section className="evaluation-results shell" aria-labelledby="tasks-title">
        <div className="evaluation-results-intro">
          <div className="section-label">EVALUATION TASKS</div>
          <h2 id="tasks-title">Tasks grouped by what the agent claims to operate.</h2>
          <p>
            Evaluation tasks are organized by task family — capacity and scaling, workload
            health, networking and dependencies, progressive delivery — so a qualification
            suite maps directly to claimed capabilities.
          </p>
          <div className="hero-actions">
            <a className="button" href="/scenarios">Browse evaluation tasks</a>
            <a className="button button-secondary" href="/scenarios/new">Define a custom task</a>
          </div>
        </div>

        <div className="task-family-list">
          <article><span>KUBERNETES</span><h3>Capacity &amp; scaling</h3><p>Traffic saturation, connection backpressure, and scaling decisions under downstream pressure.</p></article>
          <article><span>KUBERNETES</span><h3>Workload health</h3><p>Degraded workloads, resource pressure, and recovery without expanding blast radius.</p></article>
          <article><span>KUBERNETES</span><h3>Networking / dependencies</h3><p>Upstream faults and connectivity degradation requiring diagnosis before remediation.</p></article>
          <article><span>DELIVERY</span><h3>Progressive delivery</h3><p>Canary degradation and failed rollouts. Requires the GitOps environment (planned).</p></article>
          <article><span>OBSERVABILITY</span><h3>Diagnosis / telemetry</h3><p>Tasks that test whether the agent can locate the fault before acting.</p></article>
          <article><span>SAFETY</span><h3>Operational boundaries</h3><p>Every task enforces forbidden behaviors: no unrelated changes, no disabled monitoring, no unsafe scaling.</p></article>
        </div>
      </section>

      <section className="environments shell" id="environments" aria-labelledby="environments-title">
        <div className="decisions-intro">
          <div className="section-label">ENVIRONMENTS</div>
          <h2 id="environments-title">Test against the infrastructure the capability actually requires.</h2>
          <p className="flow-lead">
            Tasks run in the infrastructure required by the capability — isolated from
            production and reset between trials.
          </p>
        </div>
        <div className="environment-list">
          <article>
            <div><h3>Kubernetes Sandbox</h3><span className="status-available">AVAILABLE</span></div>
            <p>The current foundation for qualification trials: isolated namespaces with realistic workloads, traffic, and telemetry.</p>
          </article>
          <article>
            <div><h3>GitOps Environment</h3><span className="status-planned">PLANNED</span></div>
            <p>Git + Argo CD + Kubernetes + Prometheus for progressive-delivery tasks. Currently building — not yet available.</p>
          </article>
          <article>
            <div><h3>Dedicated Kubernetes</h3><span className="status-planned">PLANNED</span></div>
            <p>Reserved capacity for node-level and cluster-wide failure tasks. Planned — not yet available.</p>
          </article>
          <article>
            <div><h3>AWS Sandbox</h3><span className="status-future">FUTURE</span></div>
            <p>Cloud-control-plane tasks. Future — not currently offered or priced.</p>
          </article>
        </div>
      </section>

      <section className="custom-scenarios shell" aria-labelledby="connect-title">
        <div className="custom-scenarios-intro">
          <div className="section-label">CONNECT AN AGENT</div>
          <h2 id="connect-title">Connect an infrastructure agent and qualify the capabilities it claims to support.</h2>
          <p>
            Register an exact agent version over HTTP with direct or MCP tool access, declare
            its capability claims, and run it through a qualification suite with repeated trials.
          </p>
          <div className="hero-actions">
            <a className="button" href="/agents/new">Connect an agent</a>
            <a className="button button-secondary" href="/pricing">See pricing</a>
          </div>
        </div>
      </section>

      <footer className="site-footer">
        <div className="footer-inner shell">
          <div className="footer-main">
            <div className="footer-brand">
              <a className="footer-wordmark" href="/" aria-label="Evalplane home">evalplane</a>
              <p>Qualify infrastructure agents before they reach production.</p>
            </div>

            <nav className="footer-navigation" aria-label="Footer navigation">
              <div>
                <h2>PRODUCT</h2>
                <a href="/#how-qualification-works">How qualification works</a>
                <a href="/scenarios">Evaluation Tasks</a>
                <a href="/#environments">Environments</a>
                <a href="/pricing">Pricing</a>
              </div>
              <div>
                <h2>BUILDERS</h2>
                <a href="/agents/new">Connect an agent</a>
                <a href="/scenarios/new">Define a task</a>
                <a href="/runs/new">Start a trial</a>
              </div>
              <div>
                <h2>ACCOUNT</h2>
                <a href="/login">Sign in</a>
                <a href="/signup">Get started</a>
              </div>
            </nav>
          </div>

          <div className="footer-bottom">
            <span>© 2026 evalplane</span>
            <div className="footer-social">
              <a href="/pricing">Pricing</a>
              <a href="/scenarios">Evaluation Tasks</a>
            </div>
          </div>
        </div>
      </footer>
    </main>
  );
}
