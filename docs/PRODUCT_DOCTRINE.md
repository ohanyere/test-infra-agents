# Product Doctrine

## Category

We verify autonomous infrastructure changes.

## Initial wedge

Infrastructure and SRE agents that autonomously modify Kubernetes and
cloud-native infrastructure. The first deep vertical is Kubernetes / SRE
remediation verification. We do not expand broadly until this verifier is
proven.

## Why this exists

Historically, a human proposed an infrastructure change, reviewed it, observed
the result, and investigated side effects. Increasingly, autonomous systems and
AI agents can propose and execute infrastructure changes directly. As the
number and velocity of those changes increase, humans cannot manually verify
every resulting infrastructure state.

A successful tool call, command, API response, agent trace, reasoning
trajectory, or completed task does not prove that infrastructure was left in a
correct state.

**Autonomous infrastructure change requires independent verification.**

## Core product question

After an autonomous system changes infrastructure:

**What state did it leave the system in?**

Not: Did the agent say it succeeded? Did the command return zero? Did the
Kubernetes API return 200? Did an LLM evaluator approve the answer? Did the
agent follow an expected reasoning path?

We independently verify infrastructure reality.

## Verification model

### Pre-change state

What was true before the autonomous action?

### Intent

What operational outcome was supposed to occur?

### Action attribution

What was requested? What was authorized? What actually executed? Which
resulting mutations can be attributed to the autonomous actor?

### Actual state delta

What infrastructure actually changed?

### Outcome

Did the intended operational result become true?

### Invariants

What conditions must remain true regardless of how the autonomous system
solves the task?

### Collateral effects

What else changed or degraded?

### Stability

Did the resulting state remain acceptable after controllers, dependencies, and
reconciliation reacted?

### Residual risk

What credible operational risk exists because of the resulting state?

### Verification result

- `VERIFIED`
- `WARNING`
- `FAILED`
- `INCONCLUSIVE`

## The fundamental distinction

An autonomous system can successfully complete its task while leaving
infrastructure in a bad state.

Canonical example: the objective is to recover checkout after a failed
deployment. The autonomous actor patches the live Kubernetes Deployment from
image v18 to v17. Checkout recovers. The actor reports success, and ordinary
agent evaluation passes.

Independent infrastructure verification finds that Git still declares v18,
Argo CD is OutOfSync, desired and live state diverged, the remediation is not
durable, and controller reconciliation can restore the broken version. The
verification result is `FAILED`.

This is exactly the class of failure the product must become exceptional at
finding.

## Development mode

During autonomous-agent development, a developer changes agent code, model,
prompt, tools, policies, or permissions. Then:

agent version → controlled infrastructure test → autonomous change →
independent verification → resulting-state evidence → version comparison.

The customer should learn what bad states a version repeatedly leaves behind,
which invariants it violates, which remediations are unstable, which
dependencies it damages, which residual risks recur, and whether a new version
leaves infrastructure in better states.

Scenarios and Test Cases are supporting test infrastructure. They are not the
product category.

## Production mode

In production, the customer's autonomous system stays in the customer's
runtime and performs real infrastructure work. Our integration observes the
relevant change window and independently verifies the resulting state. The
customer receives a history of autonomous infrastructure changes and the
quality of the states those changes produced.

Production verification should not require us to own the customer's agent,
model, reasoning runtime, or orchestration system, or to become their SRE
agent.

## Product boundary

We are not:

- a generic agent evaluation platform;
- an LLM observability platform;
- an agent tracing company;
- an agent memory system;
- a generic simulation company;
- an agent marketplace;
- an AI SRE;
- an agent orchestration platform;
- a prompt-debugging product.

We can integrate with those systems. We verify their infrastructure
consequences.

## Scenarios

Scenarios exist because development-time verification requires reproducible
infrastructure situations: failed rollouts, traffic or capacity pressure, node
failure, dependency degradation, GitOps drift, and configuration regression.

A scenario is useful only because it creates an autonomous infrastructure
change whose resulting state can be independently verified. Do not position
scenarios as the company.

## Independence

Never trust the autonomous actor as the source of truth for its own success.

Infrastructure-native evidence should increasingly come from Kubernetes
API/state, Prometheus and SLOs, Git desired state, Argo CD and controller state,
Terraform or cloud state, cloud audit events, resource ownership, dependency
telemetry, autoscalers, and capacity or quota information.

The actor's action log is evidence of intent, request, and execution. It is not
proof of outcome, safety, or durability.

## Product moat

Our technical depth should increasingly be infrastructure state modeling,
state differencing, deterministic attribution, infrastructure invariants,
controller and reconciliation awareness, dependency consequence analysis,
stability verification, residual-risk analysis, longitudinal state-quality
analysis, and production autonomous-change verification.

Simulation infrastructure is supporting machinery. **The verifier is the
product.**

## Backend feature gate

Before adding a backend capability ask:

> Does this help us detect, prove, explain, or prevent a bad resulting
> infrastructure state that an ordinary agent evaluation could miss?

If no, do not build it unless it is strictly required to operate the
verification system.

## Frontend feature gate

Before giving a feature primary UI surface area ask:

> Does this help the customer understand the quality or consequences of an
> autonomous infrastructure change?

If no, do not give it primary product surface area.

## First proof standard

The first undeniable proof must demonstrate: an autonomous system acts; its
action reports success; ordinary evaluation would appear successful; our
verifier independently inspects infrastructure reality; and our verifier finds
an important bad resulting state.

The strongest first proof is GitOps remediation. The agent patches live
Kubernetes, the application recovers, and the agent says success. The verifier
discovers Git/live divergence, Argo OutOfSync, non-durable remediation, and
reconciliation risk. Final result: `FAILED`.

## Expansion rule

Company thesis: **Verify autonomous infrastructure changes.**

Initial implementation wedge: Kubernetes/SRE remediation verification, then
GitOps verification.

Only after demonstrated customer need should we consider Terraform, AWS,
Azure, GCP, database infrastructure, network infrastructure, or other
autonomous infrastructure systems. Do not broaden prematurely.

## North-star statement

**VERIFY AUTONOMOUS INFRASTRUCTURE CHANGES.**

Supporting statement: Independently prove that an autonomous infrastructure
change worked, left the system stable, preserved critical invariants, and did
not introduce unacceptable risk.

Agent-specific statement: Your infrastructure agent says it fixed the problem.
Verify the state it left behind.
