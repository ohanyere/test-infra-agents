"use client";

import { useCallback, useEffect, useState } from "react";
import { APIError, apiRequest, type CertificationStatus } from "../../lib/api";
import { useAuth } from "../../lib/auth";

const stateLabel: Record<string, string> = {
  uncertified: "UNCERTIFIED",
  certifying: "CERTIFYING",
  certified: "CERTIFIED",
  failed: "FAILED",
  stale: "STALE",
};

const gateLabel: Record<string, string> = {
  initial_state: "Initial state",
  reference_solvability: "Reference solvability",
  grader_positive: "Grader positive check",
  grader_negative: "Grader negative check",
  safety_detection: "Safety detection",
  telemetry: "Telemetry",
  repeatability: "Repeatability",
  reset_cleanliness: "Reset cleanliness",
  variant_validation: "Variant validation",
  leakage: "Leakage",
  criteria_validation: "Criteria validation",
};

export function TaskCertification({ scenarioID, versionID }: { scenarioID: string; versionID: string }) {
  const { workspace } = useAuth();
  const [status, setStatus] = useState<CertificationStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!workspace) return;
    try {
      const value = await apiRequest<CertificationStatus>(
        `/v1/workspaces/${workspace.id}/scenarios/${scenarioID}/versions/${versionID}/certification`,
      );
      setStatus(value);
    } catch (reason) {
      if (reason instanceof APIError && reason.status === 404) {
        setStatus(null);
        return;
      }
      setError(reason instanceof APIError ? reason.message : "Certification status could not be loaded.");
    }
  }, [workspace, scenarioID, versionID]);

  useEffect(() => {
    void load();
  }, [load]);

  async function trigger() {
    if (!workspace) return;
    setBusy(true);
    setError("");
    try {
      await apiRequest(`/v1/workspaces/${workspace.id}/scenarios/${scenarioID}/versions/${versionID}/certification`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      await load();
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : "Certification could not be triggered.");
    } finally {
      setBusy(false);
    }
  }

  const effective = status?.effective_state ?? "uncertified";
  const gates = status?.latest?.gate_results ?? [];
  const canWrite = workspace && workspace.role !== "viewer";

  return (
    <section aria-label="Task certification">
      <p>
        Certification{" "}
        <strong>{stateLabel[effective] ?? effective.toUpperCase()}</strong>
      </p>
      {!status?.eligible_authoritative_evidence && (
        <p>Uncertified tasks are runnable but are not production qualification evidence.</p>
      )}
      {status?.stale && status.stale_reasons.length > 0 && <p>Stale: {status.stale_reasons.join("; ")}.</p>}
      {gates.length > 0 && (
        <dl>
          {gates.map((gate) => (
            <div key={gate.gate}>
              <dt>{gateLabel[gate.gate] ?? gate.gate}</dt>
              <dd>{gate.status === "passed" ? "Passed" : gate.status === "failed" ? "Failed" : "Not applicable"}</dd>
            </div>
          ))}
        </dl>
      )}
      {canWrite && (
        <button type="button" onClick={trigger} disabled={busy}>
          {busy ? "Certifying…" : "Run certification"}
        </button>
      )}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
