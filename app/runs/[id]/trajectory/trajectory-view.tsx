"use client";

import { useCallback, useEffect, useState } from "react";
import { useAuth } from "../../../../lib/auth";
import { APIError, EvidenceDetail, Trajectory, getTrajectory, getTrajectoryEvidence } from "../../../../lib/api";
import styles from "./trajectory.module.css";

function relative(event: { relative_ms?: number | null }): string {
  if (event.relative_ms === undefined || event.relative_ms === null) return "—";
  const total = Math.max(0, Math.floor(event.relative_ms / 1000));
  const minutes = String(Math.floor(total / 60)).padStart(2, "0");
  const seconds = String(total % 60).padStart(2, "0");
  return `+${minutes}:${seconds}`;
}

function formatDuration(ms?: number | null): string {
  if (ms === undefined || ms === null) return "—";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function outcomeClass(validity: string, outcome: string): string {
  if (validity === "invalid_platform") return styles.outcomeInvalid;
  if (outcome === "fail") return styles.outcomeFail;
  if (outcome === "pass") return styles.outcomePass;
  return "";
}

function outcomeLabel(validity: string, outcome: string): string {
  if (validity === "invalid_platform") return "INVALID — EVALUATION PLATFORM";
  return outcome.toUpperCase();
}

function explanation(summary: Trajectory["summary"]): string[] {
  const lines: string[] = [];
  if (summary.validity === "invalid_platform") {
    lines.push("Required evaluation evidence became unavailable, so this trial was marked platform-invalid.");
    lines.push("Agent reliability was not penalized.");
    return lines;
  }
  if (summary.time_to_recovery_ms !== null && summary.time_to_recovery_ms !== undefined) {
    lines.push(`Recovery succeeded after ${formatDuration(summary.time_to_recovery_ms)}${summary.recovery_verified ? " and was verified" : ""}.`);
  } else {
    lines.push("Recovery criteria were not satisfied in the evaluation window.");
  }
  if (summary.actions_after_recovery > 0) {
    lines.push(`The agent then performed ${summary.actions_after_recovery} additional mutation(s) after recovery.`);
  }
  if (summary.safety_violation_count > 0) {
    lines.push(`${summary.safety_violation_count} attempted action(s) targeted out-of-scope resources and were denied.`);
  }
  return lines;
}

export function TrajectoryView({ runID }: { runID: string }) {
  const { workspace } = useAuth();
  const [data, setData] = useState<Trajectory | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [phase, setPhase] = useState("");
  const [actor, setActor] = useState("");
  const [status, setStatus] = useState("");
  const [offset, setOffset] = useState(0);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [detail, setDetail] = useState<EvidenceDetail | null>(null);
  const limit = 100;

  const load = useCallback(async () => {
    if (!workspace) return;
    setLoading(true);
    setError(null);
    try {
      const value = await getTrajectory(workspace.id, runID, {
        limit, offset,
        ...(phase ? { phase } : {}),
        ...(actor ? { actor } : {}),
        ...(status ? { status } : {}),
      });
      setData(value);
    } catch (err) {
      setError(err instanceof APIError ? err.message : "The trajectory could not be loaded.");
    } finally {
      setLoading(false);
    }
  }, [workspace, runID, offset, phase, actor, status]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { setOffset(0); }, [phase, actor, status]);

  async function toggle(eventID: string) {
    if (expanded === eventID) {
      setExpanded(null);
      setDetail(null);
      return;
    }
    setExpanded(eventID);
    setDetail(null);
    if (!workspace) return;
    try {
      setDetail(await getTrajectoryEvidence(workspace.id, runID, eventID));
    } catch {
      setDetail(null);
    }
  }

  if (!workspace) return <main className={styles.state} role="status">Sign in to inspect trial trajectories.</main>;
  if (loading && !data) return <main className={styles.state} role="status">Loading trajectory…</main>;

  return (
    <div className={styles.shell}>
      <nav className={styles.crumbs} aria-label="Breadcrumb">
        <a href="/workspace">Workspace</a><span>/</span>
        {data?.pins.suite_execution_id ? <><a href={`/suite-executions/${data.pins.suite_execution_id}`}>SuiteExecution</a><span>/</span></> : null}
        <span>Trial {runID.slice(0, 8)}</span><span>/</span><span>Trajectory</span>
      </nav>
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {data ? (
        <>
          <header className={styles.header}>
            <p className={styles.kicker}>TRIAL TRAJECTORY · {data.contract}</p>
            <h1 className={styles.title}>Trial {data.run_id.slice(0, 8)}</h1>
            <div className={styles.meta}>
              <span>trial {data.pins.trial_ordinal}</span>
              <span>agent {data.pins.agent_version_id.slice(0, 8)}</span>
              <span>scenario {data.pins.scenario_version_id.slice(0, 8)}</span>
              <span>evaluator {data.pins.evaluator_contract}</span>
            </div>
          </header>

          <section className={`${styles.outcome} ${outcomeClass(data.summary.validity, data.summary.pass_fail_outcome)}`} aria-label="Outcome">
            <strong>{outcomeLabel(data.summary.validity, data.summary.pass_fail_outcome)}</strong>
            <div>
              {explanation(data.summary).map((line) => <p className={styles.explanation} key={line}>{line}</p>)}
              {data.summary.failure_classes.length > 0 ? (
                <p className={styles.explanation}>Failure classification: {data.summary.failure_classes.join(", ")}</p>
              ) : null}
            </div>
          </section>

          <section className={styles.summaryGrid} aria-label="Evidence summary">
            {[
              ["REQUESTED", String(data.summary.actions_requested)],
              ["EXECUTED", String(data.summary.actions_executed)],
              ["DENIED", String(data.summary.actions_denied)],
              ["OBSERVATIONS", String(data.summary.observations_count)],
              ["TIME TO RECOVERY", formatDuration(data.summary.time_to_recovery_ms)],
              ["AFTER RECOVERY", String(data.summary.actions_after_recovery)],
              ["SAFETY", String(data.summary.safety_violation_count)],
              ["TRIAL DURATION", formatDuration(data.durations.trial_duration_ms)],
            ].map(([label, value]) => (
              <div className={styles.summaryCell} key={label}><span>{label}</span><strong>{value}</strong></div>
            ))}
          </section>

          <div className={styles.filters} role="group" aria-label="Trajectory filters">
            <select aria-label="Phase" value={phase} onChange={(event) => setPhase(event.target.value)}>
              <option value="">all phases</option>
              {["provisioning", "setup", "agent_execution", "verification", "evaluation", "cleanup"].map((value) => <option key={value} value={value}>{value}</option>)}
            </select>
            <select aria-label="Actor" value={actor} onChange={(event) => setActor(event.target.value)}>
              <option value="">all actors</option>
              {["platform", "agent", "environment", "tool", "evaluator"].map((value) => <option key={value} value={value}>{value}</option>)}
            </select>
            <select aria-label="Status" value={status} onChange={(event) => setStatus(event.target.value)}>
              <option value="">all statuses</option>
              {["success", "failed", "denied", "info", "finding", "invalid"].map((value) => <option key={value} value={value}>{value}</option>)}
            </select>
          </div>

          <section className={styles.timeline} aria-label="Timeline">
            {data.events.map((event) => (
              <div key={event.event_id}>
                <div className={styles.row}>
                  <span className={styles.time}>{relative(event)}</span>
                  <span className={styles.actor}>{event.actor}</span>
                  <span className={styles.event}>
                    <button className={styles.toggle} type="button" onClick={() => void toggle(event.event_id)} aria-expanded={expanded === event.event_id}>
                      {event.summary}
                    </button>
                    <small>{event.event_type} · {event.phase} · {event.view === "evaluator" ? "evaluator evidence" : "agent-visible"}</small>
                  </span>
                  <span className={`${styles.status} ${styles["status" + event.status[0].toUpperCase() + event.status.slice(1)] ?? ""}`}>{event.status}</span>
                  {expanded === event.event_id ? (
                    <div className={styles.detail}>
                      {`source ${event.source_type}:${event.source_id}\nabsolute ${event.occurred_at}\nprovenance ${event.evidence_refs.join(", ")}`}
                      {detail && detail.evidence_id === event.event_id ? (
                        `\n\n${detail.preview ?? JSON.stringify(detail.safe_detail)}${detail.truncated ? `\n…truncated (${detail.byte_size} bytes, sha256 ${detail.content_hash.slice(0, 16)}…)` : ""}`
                      ) : "\n\nloading evidence…"}
                    </div>
                  ) : null}
                </div>
              </div>
            ))}
          </section>

          <div className={styles.pager}>
            <button type="button" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>Previous</button>
            <span>{offset + 1}–{offset + data.events.length} of {data.total_events}{data.truncated ? " (more available)" : ""}</span>
            <button type="button" disabled={!data.truncated} onClick={() => setOffset(offset + limit)}>Next</button>
          </div>
          <p className={styles.crumbs}>Ordering: {data.ordering_rule}</p>
        </>
      ) : null}
    </div>
  );
}
