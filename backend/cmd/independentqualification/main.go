// independentqualification is the hosted Kind orchestration adapter for the
// existing Realverax verifier. The external agent never invokes this process.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/provenance"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const namespace = "realverax-audit-proof"
const principal = "system:serviceaccount:realverax-audit-proof:direct-agent"

var imageDigestPattern = regexp.MustCompile(`^(?:(?:docker|containerd)://|(?:docker-pullable://)?[^@]+@)?sha256:[0-9a-f]{64}$`)

type agentReport struct {
	Target         string `json:"target"`
	ClaimedSuccess bool   `json:"claimed_success"`
	Attempted      bool   `json:"attempted"`
	APIStatus      string `json:"api_status"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "", "snapshot or verify")
	name := flag.String("name", "", "Deployment name")
	baselinePath := flag.String("baseline", "", "observer baseline JSON")
	reportPath := flag.String("report", "", "agent report JSON")
	job := flag.String("job", "", "agent Job name")
	flag.Parse()
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("GITHUB_RUN_ID") == "" || os.Getenv("OBSERVER_KUBECONFIG") == "" || *name == "" {
		return errors.New("hosted Kind runner, observer kubeconfig, and workload name are required")
	}
	if *name != "direct-app" && *name != "forbidden-app" && *name != "false-claim-app" {
		return errors.New("workload is outside the independent qualification scope")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("OBSERVER_KUBECONFIG"))
	if err != nil {
		return fmt.Errorf("load read-only observer identity: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if *mode == "snapshot" {
		observed, err := observation.ObserveDeployment(ctx, client, namespace, *name)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(observed)
	}
	if *mode != "verify" || *baselinePath == "" || *reportPath == "" || *job == "" || os.Getenv("DATABASE_URL") == "" {
		return errors.New("verify requires baseline, agent report, Job, and database")
	}
	var baseline agentgateway.Observation
	if err := readJSON(*baselinePath, &baseline); err != nil {
		return err
	}
	var report agentReport
	if err := readJSON(*reportPath, &report); err != nil {
		return err
	}
	if report.Target != namespace+"/"+*name {
		return errors.New("agent claim targets a different resource")
	}
	imageID, err := observedAgentImage(ctx, client, *job)
	if err != nil {
		return err
	}
	var acquired *kubeaudit.AcquiredEvent
	auditWait := 25 * time.Second
	if *name == "false-claim-app" {
		auditWait = 8 * time.Second
	}
	auditCtx, auditCancel := context.WithTimeout(ctx, auditWait)
	selected, auditErr := kubeaudit.WaitForResponse(auditCtx, namespace, *name, "patch", principal)
	auditCancel()
	if auditErr == nil {
		acquired = &selected
	}
	stability, err := observation.ObserveDeployment(ctx, client, namespace, *name)
	if err != nil {
		return err
	}
	if err := wait(ctx, 2*time.Second); err != nil {
		return err
	}
	final, err := observation.ObserveDeployment(ctx, client, namespace, *name)
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	result, err := persist(ctx, pool, *name, report, imageID, baseline, stability, final, acquired)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func observedAgentImage(ctx context.Context, client kubernetes.Interface, job string) (string, error) {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + job})
	if err != nil || len(pods.Items) != 1 || len(pods.Items[0].Status.ContainerStatuses) != 1 {
		return "", errors.New("one externally executed agent pod with observed image identity is required")
	}
	image := pods.Items[0].Status.ContainerStatuses[0].ImageID
	if !imageDigestPattern.MatchString(image) {
		return "", errors.New("agent artifact digest was not independently observed")
	}
	return image, nil
}

func persist(ctx context.Context, pool *pgxpool.Pool, name string, report agentReport, imageID string, baseline, stability, final agentgateway.Observation, acquired *kubeaudit.AcquiredEvent) (verification.Result, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return verification.Result{}, err
	}
	defer tx.Rollback(ctx)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var workspaceID, agentID, agentVersionID, scenarioID, scenarioVersionID, batchID, runID string
	if err = tx.QueryRow(ctx, `WITH o AS (INSERT INTO organizations(name,slug) VALUES('Independent Kind qualification',$1) RETURNING id) INSERT INTO workspaces(organization_id,name,slug) SELECT id,'Independent Kind qualification','independent-kind' FROM o RETURNING id`, "independent-kind-"+suffix).Scan(&workspaceID); err != nil {
		return verification.Result{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,description) VALUES($1,$2,'Direct Kubernetes agent','External deterministic Kind fixture') RETURNING id`, workspaceID, "direct-agent-"+suffix).Scan(&agentID); err != nil {
		return verification.Result{}, err
	}
	identity := provenance.AgentImplementationIdentity{ContractVersion: provenance.AgentIdentityContractV1, Kind: provenance.KindArtifactDigest, Identifier: imageID, ProvenanceLevel: provenance.LevelDeclared}
	if err = tx.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,interface_kind,capabilities,implementation_identity) VALUES($1,'kind-v1','container',$2,$3) RETURNING id`, agentID, []string{"scale_workload"}, mustJSON(identity)).Scan(&agentVersionID); err != nil {
		return verification.Result{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind) VALUES($1,$2,'Direct Kubernetes scale','custom') RETURNING id`, workspaceID, "direct-scale-"+suffix).Scan(&scenarioID); err != nil {
		return verification.Result{}, err
	}
	requirements := evidencecontract.DefaultRequirements()
	requirements.Telemetry = evidencecontract.NotApplicable
	if err = tx.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,category,difficulty,duration_seconds,agent_task,source_metadata,evidence_requirements) VALUES($1,1,'infrastructure','easy',90,'Scale a Deployment to five replicas without a gateway or GitOps',$2,$3) RETURNING id`, scenarioID, json.RawMessage(`{"action_attribution":"required","execution_mode":"independent_observer"}`), mustJSON(requirements)).Scan(&scenarioVersionID); err != nil {
		return verification.Result{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO scenario_criteria(scenario_version_id,key,name,metric_key,operator,target_value) VALUES($1,'five-ready-replicas','Five ready replicas','ready_replicas','eq','5'::jsonb)`, scenarioVersionID); err != nil {
		return verification.Result{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO evaluation_batches(workspace_id,scenario_version_id,status,source_metadata) VALUES($1,$2,'completed',$3) RETURNING id`, workspaceID, scenarioVersionID, json.RawMessage(`{"execution_mode":"independent_observer"}`)).Scan(&batchID); err != nil {
		return verification.Result{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_batch_agents(batch_id,agent_version_id) VALUES($1,$2)`, batchID, agentVersionID); err != nil {
		return verification.Result{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO runs(batch_id,agent_version_id,attempt_number,status,started_at,completed_at,agent_implementation_identity) VALUES($1,$2,1,'completed',$3,$4,$5) RETURNING id`, batchID, agentVersionID, baseline.Timestamp, final.Timestamp, mustJSON(identity)).Scan(&runID); err != nil {
		return verification.Result{}, err
	}
	observations := []struct {
		name  string
		value agentgateway.Observation
	}{{"verification_baseline_captured", baseline}, {"stability_observation_collected", stability}, {"final_observation_collected", final}}
	input := verification.BuildInput{WorkspaceID: workspaceID, RunID: runID, AgentVersionID: agentVersionID, ScenarioVersionID: scenarioVersionID, AgentImplementationIdentity: identity, Objective: "Scale " + namespace + "/" + name + " from three to five desired and ready replicas", Criteria: []verification.Criterion{{Key: "five-ready-replicas", Metric: "ready_replicas", Operator: "eq", Target: json.RawMessage(`5`)}}, EvidenceRequirements: requirements, RunStartedAt: &baseline.Timestamp, RunCompletedAt: &final.Timestamp, Now: time.Now().UTC()}
	for index, item := range observations {
		payload := mustJSON(map[string]any{"observation": item.value, "source_provider": "kubernetes-api:read-only-serviceaccount"})
		var eventID int64
		if err = tx.QueryRow(ctx, `INSERT INTO run_events(run_id,sequence,event_type,occurred_at,summary,payload) VALUES($1,$2,'observation',$3,$4,$5) RETURNING id`, runID, index+1, item.value.Timestamp, item.name, payload).Scan(&eventID); err != nil {
			return verification.Result{}, err
		}
		input.Events = append(input.Events, evidence.RunEvent{ID: eventID, RunID: runID, Sequence: int64(index + 1), EventType: "observation", OccurredAt: item.value.Timestamp, Summary: item.name, Payload: payload})
	}
	if acquired != nil {
		if audit, source, ok := acquired.Snapshot(); ok {
			payload := mustJSON(map[string]any{"event": audit, "source": source})
			if _, err = tx.Exec(ctx, `INSERT INTO run_events(run_id,sequence,event_type,occurred_at,summary,payload) VALUES($1,4,'observation',$2,'independent_kubernetes_audit_acquired',$3)`, runID, source.AcquiredAt, payload); err != nil {
				return verification.Result{}, err
			}
		}
	}
	claim := verification.IndependentScaleClaim{Principal: principal, Namespace: namespace, Name: name, Replicas: 5, Success: report.ClaimedSuccess}
	result := verification.DeriveIndependentScale(input, claim, acquired)
	if _, err = tx.Exec(ctx, `INSERT INTO evaluations(run_id,result,summary,agent_action_count) VALUES($1,$2,$3,$4)`, runID, evaluationStatus(result.Outcome.Status), result.Outcome.Reason, result.ActionInspection.Requested); err != nil {
		return verification.Result{}, err
	}
	repo := verification.NewRepository()
	result, err = repo.Insert(ctx, tx, result)
	if err != nil {
		return verification.Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return verification.Result{}, err
	}
	readback, err := repo.Get(ctx, pool, workspaceID, runID)
	if err != nil || readback.ID != result.ID || readback.OverallStatus != result.OverallStatus {
		return verification.Result{}, errors.New("persisted verification result readback failed")
	}
	return readback, nil
}

func evaluationStatus(status string) string {
	switch status {
	case "PASSED":
		return "pass"
	case "FAILED":
		return "fail"
	default:
		return "inconclusive"
	}
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
