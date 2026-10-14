# Customer-hosted Kubernetes observation collector

This collector is read-only. It lists Deployments and Pods only in the
explicitly authorized namespaces and sends bounded snapshots to a Realverax
observation session. It cannot remediate resources. It does not collect
Prometheus, Git, or Argo evidence; scenarios requiring those channels remain
INCONCLUSIVE until those sources are connected. Observation-only sessions
require attribution and therefore cannot claim agent causation from snapshots.

## Build and install

Build the collector image from the backend module root and publish it to a
registry the customer cluster can pull from:

```sh
cd backend
docker build -f cmd/kubecollector/Dockerfile -t registry.example.com/realverax/kubecollector:v1 .
docker push registry.example.com/realverax/kubecollector:v1
```

The manifest `collector-job.yaml` contains only the bounded Job; it does not
contain a Secret or create RBAC. Replace its image with the customer's pinned
image reference. Register an environment through the workspace API, then start
a session with an existing agent version and scenario version. The session
response contains the Realverax `session_id` and `run_id`.

Create the dedicated namespace and service account, then create the namespaced
read-only Roles/RoleBindings below for each selected application namespace.
Provision `realverax-collector-credentials` through the customer's secret
manager with keys `api-url`, `session-id`, `collector-token`, and `namespaces`.
The latter is a comma-separated list matching the registered namespace scope.
Do not put credentials into the checked-in Job manifest or shell command-line
arguments. Apply `collector-job.yaml` only after the Secret and RBAC are ready.

For each authorized namespace, create only the following namespaced RBAC grants
(repeat the Role and RoleBinding commands for every namespace):

```sh
kubectl create namespace realverax-system
kubectl create serviceaccount realverax-collector --namespace realverax-system
kubectl create role realverax-observe --namespace APP_NAMESPACE \
  --verb=get,list --resource=deployments,pods
kubectl create rolebinding realverax-observe --namespace APP_NAMESPACE \
  --role=realverax-observe \
  --serviceaccount=realverax-system:realverax-collector
# provision the named Secret using the customer's secret-management procedure
kubectl apply -f collector-job.yaml
```

The collector requires outbound HTTPS connectivity to the configured Realverax
API. It uses the in-cluster service-account token for Kubernetes API access.
No cluster-admin binding, mutation verb, kubeconfig upload, or inbound network
path is required. For a local mocked API demonstration, use a test server and
fake Kubernetes client in Go tests; do not use a production cluster.

The opaque credential authenticates a registered environment/session, but it
does not cryptographically attest the Kubernetes cluster or make client-supplied
observations tamper-proof. The configured cluster identifier is an operator
label. Treat the data as authenticated customer-collector evidence with stated
provenance, not as independent hardware or control-plane attestation.

## Session and evidence contract

1. Register: `POST /v1/workspaces/{workspace}/customer-kubernetes-environments`
   with `name`, `cluster_identifier`, `environment_tier` (`staging` or
   `production`) and an explicit namespace list. The collector credential is
   returned once; only its SHA-256 digest is persisted.
2. Start observation: `POST /v1/workspaces/{workspace}/customer-kubernetes-environments/{environment}/observation-sessions`
   with `agent_version_id` and `scenario_version_id`. This creates a canonical
   run in `observation_only` mode and requires action attribution.
3. Install/run the collector with the returned session ID.
4. The collector sends one baseline, at least two stability samples and one
   final batch. Set its `-duration` to at least the session's returned
   `observation_window_seconds`. The API enforces this window using server
   receipt time, not the collector's capture timestamps. Batches have unique
   IDs, strict per-session sequences, source identity assigned by the server,
   and client capture/server receipt times.
5. The final batch closes the run and invokes the existing deterministic
   evaluator followed by the existing verification service. Retrieve the
   persisted result from
   `GET /v1/workspaces/{workspace}/runs/{run_id}/verification`.

The collector currently supplies workload/pod state only. It does not send
agent action evidence or telemetry, so the canonical result is expected to be
INCONCLUSIVE rather than VERIFIED. This is not hosted or customer production
validation. The manifest uses a placeholder image until a release image is
built and published.
