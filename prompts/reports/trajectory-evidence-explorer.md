# Completion report — Trajectory & Evidence Explorer

## 1. Files changed
Created:
- `backend/internal/trajectory/contract.go` — contract version, closed actor/phase/event vocabularies, ordering rule
- `backend/internal/trajectory/model.go` — canonical event, outcome summary, durations, repro pins, evidence detail, suite summary, comparison, filters
- `backend/internal/trajectory/redact.go` — centralized redaction + bounded previews
- `backend/internal/trajectory/project.go` — deterministic pure projector (no I/O, no LLM)
- `backend/internal/trajectory/repository.go` — SQL loader over existing tables + `Service` (viewer-read RBAC, pagination, evidence detail, suite summary, comparison)
- `backend/internal/trajectory/validation.go` — closed filter/pagination validation
- `backend/internal/trajectory/project_test.go` — 14 focused projection tests
- `backend/internal/httpapi/trajectory.go` — `GET trajectory`, `GET evidence/{evidenceID}`, `GET suite trajectory-summary`, `?compare_with=` handlers
- `backend/internal/httpapi/trajectory_test.go` — 7 route tests (auth, RBAC, isolation, filters, pagination, compare)
- `app/runs/[id]/trajectory/page.tsx` + `trajectory-view.tsx` + `trajectory.module.css` — restrained trial explorer
- `app/suite-executions/[id]/page.tsx` + `suite-view.tsx` — suite summary table linking to trial trajectories
Modified:
- `backend/internal/httpapi/router.go` — `WithTrajectoryService` + 3 protected routes
- `backend/cmd/api/main.go` — trajectory service construction + router wiring
- `backend/README.md` — endpoint list kept in sync (6 entries)
- `lib/api.ts` — trajectory types + `getTrajectory`/`getTrajectoryEvidence`/`getSuiteTrajectorySummary`/`compareTrajectories`

## 2. Migration/schema
None. No new tables. The explorer is a read model over `runs`, `run_events`, `agent_actions`, `evaluations`, `evaluation_criterion_results`, `evaluation_findings`, `suite_executions`, `suite_execution_batches`, `suite_execution_run_variants`, `environment_instances`. Existing `sequence`/`created_at`/`id` columns already provide stable reproducible ordering.

## 3. Trajectory contract/version
`trajectory-evidence-contract:v1` (`trajectory.ContractVersion`), pinned on every trajectory, suite-summary, and comparison response and inside `pins.trajectory_contract`. Closed versioned vocabularies for actors (5), phases (6), event types (23).

## 4. Authoritative evidence sources
`runs` (lifecycle) → `run_events` (observations/system/safety/recovery/scenario/decision/action/tool) → `agent_actions` (lifecycle) → `evaluations` + criteria + findings → `suite_execution_run_variants` (opaque variant ID only) → `environment_instances` (semantic pins only). Hidden variant configs, reference remediations, ground truth, grants, and kubeconfigs are never selected.

## 5. Canonical event model
`event_id, sequence, occurred_at (+relative_ms), event_type, phase, actor, summary, status, source_type, source_id, evidence_refs, safe_metadata, view` (`agent_visible` vs `evaluator`).

## 6. Event types/phases/actors
23 types (trial_started … trial_completed), 6 phases (provisioning/setup/agent_execution/verification/evaluation/cleanup), 5 actors (platform/agent/environment/tool/evaluator). Unknown persisted types (e.g. `mcp`) project as safe structured tool events.

## 7. Deterministic ordering
`occurred_at ASC → source priority (run 0 … cleanup 9) → persisted sequence/stable ID ASC`, documented in `OrderingRule` and returned on every response. Sequence is display order, never fabricated causality. Test: input-order permutation yields identical output; same-timestamp tie-break verified.

## 8. Provenance
Every event carries `evidence_refs` (`run:`, `run_event:`, `agent_action:`, `evaluation:`, `criterion:`, `finding:` IDs). Evidence detail endpoint resolves any event ID or linked ref with provenance chain. Tested: no event without refs.

## 9. Observation projection
Agent-allowed channels only (metrics whitelist `request_rate_rps|error_rate_pct|p95_latency_ms|availability_pct|telemetry_available`, workload/pod counts, step). Telemetry splits into a separate `telemetry_observation`; evaluator-only enrichment never merged.

## 10. Action lifecycle projection
`proposed` → requested-only marker (never rendered as executed); `allowed` → authorized-pending + requested; `executed` → requested + executed; `failed` → requested + failed. Statuses read from the persisted row, not inferred.

## 11. Denied/safety events
`denied` → requested + `agent_action_denied` with `DENIED: <type> → <target> — <reason>` (e.g. "outside allowed resource scope"). Denials also surface as evaluator `safety_finding` via persisted safety events. Tested with `disallowed_resource`.

## 12. Tool-call projection/redaction
MCP/unknown events → `tool_invocation`/`tool_result` with safe tool name/target/status; bearer/API-key/grant/kubeconfig material redacted server-side via `RedactObject`; free-form bearer literals masked. Tested.

## 13. Agent-visible vs evaluator evidence separation
`view: agent_visible` (observations, requests, executions, tool results) vs `view: evaluator` (findings, safety, recovery, platform-invalid, evaluation completion). Tested.

## 14. State-change derivation
Derived only from explicit `state_transition`/`state_changes` objects inside an observation payload, with mandatory evidence link. Plain observations yield zero state events. Tested both directions.

## 15. Recovery detection/verification
`recovery_detected_at` = earliest `recovery`-type event or explicit recovery code; `recovery_verified_at` only from `recovery_verified`/`worker_recovery_completed` at/after detection. `recovery_duration_ms` fallback only when evaluator evidence exists. No invention: empty evidence → nil. Tested.

## 16. Actions-after-recovery
`actions_before_recovery`/`actions_after_recovery` split by `started_at` vs `recovery_detected_at`. Exposed in summary only; no automatic trial failure.

## 17. Platform-invalid representation
Validity reuses the shared classification: platform-class run events (excluding `cleanup_completed`) or `runtime_failure`/`telemetry_unavailable` findings on a terminal/evaluated trial → `invalid_platform`, projected as `platform_invalid` events. Tested.

## 18. Outcome summary
`trial_status, evaluation_status, validity, pass_fail_outcome, failure_classes (failure-taxonomy categories), finding_count, safety_violation_count, actions_requested/executed/denied, observations_count, time_to_recovery_ms, recovery_verified, actions_after_recovery/before`. No qualification verdict.

## 19. Duration metrics
Trial, env-setup, agent-active, time-to-first-action, time-to-recovery, verification — each computed only from present evidence, else explicit null. Tested (incl. missing → null).

## 20. Evidence detail API
`GET …/runs/{runID}/evidence/{evidenceID}` → safe detail, bounded preview, truncation flag, byte size, sha256 hash, provenance. Unknown IDs → 404.

## 21. Payload bounding/truncation
`MaxPreviewBytes = 4096`; `BoundedPreview` returns preview + truncated + byte size + hash. Main trajectory metadata is redacted summaries, never raw logs. Tested (oversize truncates, small passes through).

## 22. Redaction
Centralized in `trajectory/redact.go`: authorization/cookie/api_key/token/bearer/password/secret/kubeconfig/credential/connection/tool_access/grant keys (exact or suffix match), bearer/basic literals, deep recursion. Server-side only; frontend never hides secrets. Tested (secrets gone, operational context intact).

## 23. GitOps/delivery trajectory support
No live GitOps claims; no fabricated rollout/sync events. State transitions project only from explicit observation payloads (same gate as §14); unavailable-task trials emit none. Tested (empty evidence → zero state events).

## 24. Trajectory comparison
`GET …/trajectory?compare_with={runID}` → summary-level comparison (actions, denied, time-to-recovery, verified, findings, outcome, failure classes). No diff engine, no reasoning comparison. UUID-validated.

## 25. SuiteExecution summary
`GET …/suite-executions/{executionID}/trajectory-summary` → per-trial rows (outcome, validity, actions, safety, recovery) each linking to `/runs/{runID}/trajectory`. Frontend `/suite-executions/[id]` renders the compact table. Not a dashboard.

## 26. API/RBAC
All four endpoints are workspace-scoped reads under existing auth + origin middleware; any workspace member including viewer may read (`GetRole` presence check, `ErrNotFound` → 403); cross-workspace → 404 via workspace-predicate SQL. No mutations.

## 27. Pagination/filtering
Stable total ordering; `limit` 1–100, `offset` ≥ 0; response reports `total_events` + `truncated` so prefixes are never silent. Filters: `phase|event_type|actor|status` against closed vocabularies only — no arbitrary query language. Tested (invalid filters → 400, bad pagination → 400).

## 28. Frontend trial trajectory
`/runs/[id]/trajectory`: header (contract, trial/agent/scenario/evaluator pins), outcome + validity + duration, 8-cell evidence summary, compact `time(actor)event(status)` rows with expandable evidence, relative `+MM:SS` times. No giant cards, terminals, neon, or bento.

## 29. Frontend evidence detail
Row expansion fetches `GET evidence/{evidenceID}`: source, absolute timestamp, safe metadata, preview with truncation notice (bytes + hash), provenance IDs. Evaluator-labeled rows marked "evaluator evidence".

## 30. Frontend outcome explanation
Composed from structured summary only (recovery time/verification, post-recovery mutations, denials, failure classes) — no LLM. Platform-invalid shows the distinct amber panel.

## 31. Frontend platform-invalid UX
`INVALID — EVALUATION PLATFORM` in amber (`#faf7ec`), copy states agent reliability was not penalized. Never red failure treatment.

## 32. Reproducibility pins
AgentVersion, ScenarioVersion, SuiteExecution/SuiteVersion, opaque hidden-variant ID only, `runtime-contract:v1`, evaluator contract (persisted row), environment profile/provider/isolation, trajectory contract, trial ordinal. No hidden contents.

## 33. Security/leakage
Workspace predicate on every query; run/action ownership re-verified in Go (`action belongs to another run` → conflict); hidden configs/reference remediation/root-cause/chain-of-thought never selected or projected (tests assert absence); secrets redacted server-side.

## 34. Focused backend tests
`trajectory`: 14 tests (contract, ordering, tie-break, requested-vs-executed, denied, redaction, agent/evaluator split, state-change gating, recovery+verified, no-invention, safety/platform-invalid, counts, missing durations, preview bounds, provenance/leakage, filters/GitOps). `httpapi`: 7 tests (auth, viewer read ×3 routes, contract, isolation, filter validation, pagination, compare validation). Full focused run: `trajectory, evidence, execution, observation, actionexec, evaluator, failuretaxonomy, evaluationsuite, httpapi, reliability` — all `ok`.

## 35. Stage-33 regression result
`reliability` package green (`ok`); no reliability code touched, no score semantics changed. Trajectory only reads the same persisted rows.

## 36. TypeScript/build
`npx tsc --noEmit` clean; `npm run build` succeeds with new routes `/runs/[id]/trajectory` and `/suite-executions/[id]`.

## 37. Confirmation requested != executed
Proposed actions project a requested marker only; executed renders solely from persisted `executed` status. Covered by `TestRequestedVsExecutedDistinction`.

## 38. Confirmation platform-invalid != agent failure
`invalid_platform` validity renders the amber `INVALID — EVALUATION PLATFORM` panel stating reliability was not penalized; failure classes keep platform vs agent taxonomy distinct. Covered by `TestSafetyAndPlatformInvalid` + frontend branch.

## 39. Confirmation no hidden variant/reference/root-cause leakage
Repository selects only `variant_id` (never `hidden_config`); projector emits no reference-remediation/root-cause fields; test scans every event ID/metadata/ref for `hidden_config|reference_remediation|root-cause|root_cause|scratchpad|chain-of-thought` — absent.

## 40. Confirmation no chain-of-thought storage/exposure
No new persistence at all; projector has no reasoning fields; `safeProposal`-style whitelisting preserved (only `{type,target,replicas}`-shaped metadata projected); tests assert no `chain-of-thought|reasoning` keys in metadata.

## 41. Confirmation no LLM explanation/judging
Zero LLM calls: pure deterministic projection + template-composed summaries from structured findings/taxonomy. No explanation/judging endpoints added.

## 42. Real-runtime limitations
Recovery/state/duration signals depend on what the worker persisted; sparse trials yield explicit nulls rather than estimates. `mcp` event types predate validation vocabulary and project under the generic tool branch. Suite summary skips trials whose rows fail to load instead of failing the whole summary. Hidden-variant identity is opaque — variant-aware analysis still lives in reliability/qualification.

## 43. Deferred work
No new migrations; no cursor pagination beyond limit/offset (volume does not yet require it); no aligned event-level diff; no video/session/log product; no GitOps live provider; no governance/billing/CI changes — all per §39 DO NOT IMPLEMENT.
