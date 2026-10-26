#!/usr/bin/env python3
"""One disposable customer-monitoring acceptance run against a real Kind API server."""

import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import ssl
import uuid
from datetime import datetime, timezone


ROOT = Path(__file__).resolve().parents[2]
PRIVATE = Path("/tmp/realverax-customer-monitoring-private")
ARTIFACTS = Path(os.environ.get("ARTIFACT_DIR", "/tmp/verification-e2e-artifacts")) / "customer-monitoring"
NAMESPACE = "realverax-customer-proof"
DEPLOYMENT = "checkout"
API = "http://127.0.0.1:8080"
ORIGIN = "http://127.0.0.1:3007"


def run(*args, input_data=None, timeout=120):
    completed = subprocess.run(args, input=input_data, text=True, capture_output=True, timeout=timeout)
    if completed.returncode:
        raise RuntimeError(f"{args[0]} {args[1] if len(args) > 1 else ''} failed: {completed.stderr[-1200:]}")
    return completed.stdout.strip()


def kubectl(*args, input_data=None, config=None, timeout=120):
    command = ["kubectl"]
    if config:
        command += ["--kubeconfig", str(config)]
    return run(*command, *args, input_data=input_data, timeout=timeout)


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def prepare():
    os.umask(0o077)
    server_dir, apiserver_dir = PRIVATE / "server", PRIVATE / "apiserver"
    server_dir.mkdir(parents=True, exist_ok=True)
    apiserver_dir.mkdir(parents=True, exist_ok=True)
    networks = json.loads(run("docker", "network", "inspect", "bridge"))
    gateway = next(item["Gateway"] for item in networks[0]["IPAM"]["Config"] if "." in item["Gateway"])
    run("openssl", "req", "-x509", "-newkey", "rsa:2048", "-sha256", "-nodes", "-days", "1",
        "-keyout", str(server_dir / "tls.key"), "-out", str(server_dir / "tls.crt"),
        "-subj", "/CN=realverax-kind-audit", "-addext", f"subjectAltName=IP:{gateway}",
        "-addext", "extendedKeyUsage=serverAuth")
    run("openssl", "req", "-x509", "-newkey", "rsa:2048", "-sha256", "-nodes", "-days", "1",
        "-keyout", str(apiserver_dir / "client.key"), "-out", str(apiserver_dir / "client.crt"),
        "-subj", "/CN=kind-apiserver-audit", "-addext", "basicConstraints=critical,CA:FALSE",
        "-addext", "keyUsage=digitalSignature",
        "-addext", "extendedKeyUsage=clientAuth")
    shutil.copyfile(server_dir / "tls.crt", apiserver_dir / "receiver-ca.crt")
    (apiserver_dir / "webhook.kubeconfig").write_text(
        "apiVersion: v1\nkind: Config\nclusters:\n- name: realverax-audit\n  cluster:\n"
        f"    server: https://{gateway}:8443/audit\n"
        "    certificate-authority: /etc/kubernetes/realverax-audit/receiver-ca.crt\n"
        "users:\n- name: kind-apiserver-audit\n  user:\n"
        "    client-certificate: /etc/kubernetes/realverax-audit/client.crt\n"
        "    client-key: /etc/kubernetes/realverax-audit/client.key\n"
        "contexts:\n- name: kind-apiserver-audit\n  context:\n"
        "    cluster: realverax-audit\n    user: kind-apiserver-audit\n"
        "current-context: kind-apiserver-audit\n"
    )
    print("customer audit webhook credentials prepared outside artifact directory")


class CustomerAPI:
    def __init__(self):
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))

    def call(self, method, path, body=None, bearer=None):
        headers = {"Origin": ORIGIN}
        if body is not None:
            headers["Content-Type"] = "application/json"
        if bearer:
            headers["Authorization"] = "Bearer " + bearer
        request = urllib.request.Request(API + path, data=json.dumps(body).encode() if body is not None else None,
                                         headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=20) as response:
                content = response.read()
                return json.loads(content) if content else None
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"customer API {method} {path} returned {error.code}: {error.read(700).decode()}") from error


def wait_until(label, check, seconds=90):
    deadline = time.monotonic() + seconds
    last = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except (RuntimeError, urllib.error.URLError) as error:
            last = error
        time.sleep(2)
    raise RuntimeError(f"timed out waiting for {label}: {last}")


def apply(objects):
    kubectl("apply", "-f", "-", input_data=json.dumps({"apiVersion": "v1", "kind": "List", "items": objects}))


def kubernetes_setup():
    apply([
        {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": NAMESPACE}},
        *[{"apiVersion": "v1", "kind": "ServiceAccount", "metadata": {"name": name, "namespace": NAMESPACE}}
          for name in ("external-agent", "outcome-observer")],
        {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "Role", "metadata": {"name": "external-agent", "namespace": NAMESPACE},
         "rules": [{"apiGroups": ["apps"], "resources": ["deployments"], "resourceNames": [DEPLOYMENT], "verbs": ["patch"]}]},
        {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "Role", "metadata": {"name": "outcome-observer", "namespace": NAMESPACE},
         "rules": [{"apiGroups": ["apps"], "resources": ["deployments"], "resourceNames": [DEPLOYMENT], "verbs": ["get"]}]},
        *[{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "RoleBinding",
           "metadata": {"name": name, "namespace": NAMESPACE},
           "subjects": [{"kind": "ServiceAccount", "name": name, "namespace": NAMESPACE}],
           "roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": name}}
          for name in ("external-agent", "outcome-observer")],
        {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": DEPLOYMENT, "namespace": NAMESPACE},
         "spec": {"replicas": 3, "selector": {"matchLabels": {"app": DEPLOYMENT}},
                  "template": {"metadata": {"labels": {"app": DEPLOYMENT}},
                               "spec": {"containers": [{"name": "app", "image": "evalplane-reference:v17", "imagePullPolicy": "Never"}]}}}},
    ])
    kubectl("-n", NAMESPACE, "rollout", "status", "deployment/" + DEPLOYMENT, "--timeout=180s", timeout=200)
    agent = f"system:serviceaccount:{NAMESPACE}:external-agent"
    observer = f"system:serviceaccount:{NAMESPACE}:outcome-observer"
    assert kubectl("auth", "can-i", "patch", f"deployments.apps/{DEPLOYMENT}", "-n", NAMESPACE, f"--as={agent}") == "yes"
    assert kubectl("auth", "can-i", "patch", f"deployments.apps/{DEPLOYMENT}", "-n", NAMESPACE, f"--as={observer}") == "no"
    assert kubectl("auth", "can-i", "get", f"deployments.apps/{DEPLOYMENT}", "-n", NAMESPACE, f"--as={observer}") == "yes"
    token = kubectl("-n", NAMESPACE, "create", "token", "outcome-observer", "--duration=30m")
    server = kubectl("config", "view", "--raw", "--minify", "-o", "jsonpath={.clusters[0].cluster.server}")
    ca_data = kubectl("config", "view", "--raw", "--minify", "-o", "jsonpath={.clusters[0].cluster.certificate-authority-data}")
    ca = base64.b64decode(ca_data)
    observer_config = PRIVATE / "observer.kubeconfig"
    observer_config.write_text(json.dumps({"apiVersion": "v1", "kind": "Config",
        "clusters": [{"name": "kind", "cluster": {"server": server, "certificate-authority-data": ca_data}}],
        "users": [{"name": "observer", "user": {"token": token}}],
        "contexts": [{"name": "observer", "context": {"cluster": "kind", "user": "observer"}}],
        "current-context": "observer"}))
    assert kubectl("auth", "whoami", "-o", "jsonpath={.status.userInfo.username}", config=observer_config) == observer
    probe = subprocess.run(["kubectl", "--kubeconfig", str(observer_config), "-n", NAMESPACE,
                            "patch", "deployment", DEPLOYMENT, "--type=merge", "-p", '{"spec":{"replicas":5}}',
                            "--dry-run=server"], capture_output=True, text=True, timeout=20)
    assert probe.returncode != 0 and "forbidden" in probe.stderr.lower(), "observer mutation was not denied by Kubernetes RBAC"
    # openssl's binary DER must be handled without text conversion.
    leaf_bytes = subprocess.check_output(["docker", "exec", "evalplane-e2e-control-plane", "cat", "/etc/kubernetes/pki/apiserver.crt"])
    cert_der = subprocess.check_output(["openssl", "x509", "-outform", "DER"], input=leaf_bytes)
    return server, ca, token, hashlib.sha256(cert_der).hexdigest(), observer_config, agent, observer


def actual_observation(observer_config):
    deployment = json.loads(kubectl("-n", NAMESPACE, "get", "deployment", DEPLOYMENT, "-o", "json", config=observer_config))
    spec, status, metadata = deployment["spec"], deployment.get("status", {}), deployment["metadata"]
    assert status.get("readyReplicas", 0) == spec["replicas"]
    at = utc_now()
    return {"timestamp": at, "workloads": [{"kind": "Deployment", "namespace": NAMESPACE,
        "name": DEPLOYMENT, "uid": metadata["uid"], "resource_version": metadata["resourceVersion"],
        "generation": metadata["generation"], "observed_generation": status.get("observedGeneration", 0),
        "desired_replicas": spec["replicas"], "ready_replicas": status.get("readyReplicas", 0),
        "available_replicas": status.get("availableReplicas", 0), "updated_replicas": status.get("updatedReplicas", 0),
        "image": spec["template"]["spec"]["containers"][0]["image"]}],
        "pods": [], "metrics": {}, "recent_events": []}


def admin_update(value):
    deployment = json.loads(kubectl("-n", NAMESPACE, "get", "deployment", DEPLOYMENT, "-o", "json"))
    deployment["metadata"].setdefault("annotations", {})["realverax.io/monitor-proof"] = value
    deployment["metadata"].pop("managedFields", None)
    deployment.pop("status", None)
    kubectl("replace", "-f", "-", input_data=json.dumps(deployment))


def start_service(name, binary, extra_env):
    log = open(ARTIFACTS / f"{name}.log", "w")
    process = subprocess.Popen([str(binary)], cwd=ROOT / "backend", env={**os.environ, **extra_env}, stdout=log, stderr=subprocess.STDOUT)
    return process, log


def run_acceptance():
    os.umask(0o077)
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    processes = []
    logs = []
    try:
        for name in ("api", "customerauditwebhook", "assuranceworker"):
            run("go", "-C", str(ROOT / "backend"), "build", "-o", str(PRIVATE / name), "./cmd/" + name, timeout=180)
        api, api_log = start_service("api", PRIVATE / "api", {"ENVIRONMENT": "development", "APP_ORIGIN": ORIGIN,
            "COOKIE_SECURE": "false", "PORT": "8080",
            "AGENT_CREDENTIAL_ENCRYPTION_KEY": base64.b64encode(secrets.token_bytes(32)).decode()})
        processes.append(api); logs.append(api_log)
        wait_until("customer API", lambda: urllib.request.urlopen(API + "/healthz", timeout=2).status == 200)
        receiver, receiver_log = start_service("audit-receiver", PRIVATE / "customerauditwebhook", {
            "AUDIT_WEBHOOK_ADDR": ":8443", "AUDIT_WEBHOOK_TLS_CERT": str(PRIVATE / "server/tls.crt"),
            "AUDIT_WEBHOOK_TLS_KEY": str(PRIVATE / "server/tls.key")})
        processes.append(receiver); logs.append(receiver_log)
        server, ca, token, pin, observer_config, agent_principal, observer_principal = kubernetes_setup()
        customer = CustomerAPI()
        slug = "customer-proof-" + secrets.token_hex(4)
        signup = customer.call("POST", "/v1/auth/signup", {"full_name": "Customer proof owner",
            "email": slug + "@example.test", "password": secrets.token_urlsafe(28),
            "organization": {"name": "Customer proof", "slug": slug},
            "workspace": {"name": "Customer proof", "slug": "customer-proof"}})
        workspace = signup["workspace"]["id"]
        root = f"/v1/workspaces/{workspace}"
        agent = customer.call("POST", root + "/agents", {"name": "Unattributed observation placeholder",
            "slug": "observation-placeholder", "visibility": "workspace", "initial_version": {
                "version_label": "v1", "interface_kind": "unknown", "implementation_identity": {
                    "contract_version": "agent-implementation-identity:v1", "kind": "declared",
                    "identifier": "unattributed-kubernetes-principal", "provenance_level": "declared"}}})
        scenario = customer.call("POST", root + "/scenarios", {"name": "Observe direct Kubernetes operation",
            "slug": "observe-deployment", "visibility": "workspace", "initial_version": {
                "category": "traffic", "difficulty": "medium", "duration_seconds": 1,
                "agent_task": "Observe an independently executed Deployment change",
                "evidence_requirements": {"contract_version": "evidence-requirements:v1", "workload_state": "REQUIRED",
                    "telemetry": "NOT_APPLICABLE", "git_desired_state": "NOT_APPLICABLE",
                    "argo_state": "NOT_APPLICABLE", "stability": "REQUIRED"}}})
        environment = customer.call("POST", root + "/customer-kubernetes-environments", {
            "name": "Hosted Kind customer monitoring", "cluster_identifier": "kind:evalplane-e2e",
            "namespaces": [NAMESPACE], "resource_kinds": ["Deployment"], "environment_tier": "staging"})
        env_id = environment["environment"]["id"]
        base = root + "/customer-kubernetes-environments/" + env_id
        contract = customer.call("POST", base + "/operational-contracts", {"state": "active", "rules": [
            {"type": "forbid_operation", "operation": "patch", "namespace": NAMESPACE,
             "resource_kind": "Deployment", "resource_name": DEPLOYMENT},
            {"type": "min_available_replicas", "namespace": NAMESPACE, "resource_name": DEPLOYMENT, "minimum": 5}]})
        assert contract["active"]
        customer.call("POST", base + "/authenticated-audit-sources", {
            "certificate_pem": (PRIVATE / "apiserver/client.crt").read_text()})
        source = customer.call("POST", base + "/direct-outcome-sources", {"source_kind": "kubernetes_state",
            "endpoint_url": server, "server_certificate_sha256": pin,
            "namespace": NAMESPACE, "resource_name": DEPLOYMENT, "configuration": {}})
        session = customer.call("POST", base + "/observation-sessions", {
            "agent_version_id": agent["latest_version"]["id"],
            "scenario_version_id": scenario["latest_version"]["id"]})
        session_id = session["id"]
        collector_path = f"/v1/collector/observation-sessions/{session_id}/evidence"
        for sequence, phase in ((1, "baseline"), (2, "stability"), (3, "stability"), (4, "final")):
            if sequence == 4:
                time.sleep(1.2)
            observation = actual_observation(observer_config)
            ingested = customer.call("POST", collector_path, {"batch_id": str(uuid.uuid4()),
                "sequence": sequence, "schema_version": "customer-kubernetes-observation:v1",
                "phase": phase, "captured_at": observation["timestamp"], "observation": observation},
                bearer=environment["collector_credential"])
            if phase == "final":
                assert ingested["verification_status"] == "INCONCLUSIVE"
            time.sleep(0.02)
        source_dir = PRIVATE / "outcome-credentials"
        source_dir.mkdir(mode=0o700, exist_ok=True)
        (source_dir / (source["id"] + ".ca.pem")).write_bytes(ca)
        (source_dir / (source["id"] + ".token")).write_text(token)
        worker, worker_log = start_service("assurance-worker", PRIVATE / "assuranceworker", {
            "OUTCOME_CREDENTIAL_DIR": str(source_dir)})
        processes.append(worker); logs.append(worker_log)
        plan = customer.call("POST", base + f"/observation-sessions/{session_id}/monitoring", {
            "mode": "deployment_audit", "source_ids": [source["id"]], "cadence_seconds": 60,
            "duration_seconds": 600})
        assert plan["state"] == "initializing"
        monitor_path = base + f"/observation-sessions/{session_id}/monitoring"
        first_baseline = wait_until("direct pre-action baseline", lambda: (v if (v := customer.call("GET", monitor_path))["plan"].get("baseline_observation_id") else None), 90)
        assert first_baseline["plan"]["state"] == "initializing"
        # A genuine admin update proves webhook delivery before the external
        # operation. It is an update, so the customer patch prohibition remains clear.
        admin_update("monitor-ready")
        ready = wait_until("authenticated audit and ready monitoring", lambda: (v if (v := customer.call("GET", monitor_path))["plan"]["state"] == "active" else None), 100)
        assert ready["plan"]["last_audit_at"] and ready["plan"]["baseline_observation_id"] != first_baseline["plan"]["baseline_observation_id"]
        sessions_before = customer.call("GET", base + "/observation-sessions")["items"]
        assert len(sessions_before) == 1
        before_reports = customer.call("GET", base + f"/observation-sessions/{session_id}/operational-assessments")["items"]
        direct_path = base + f"/observation-sessions/{session_id}/direct-outcome-observations"
        direct_before = len(customer.call("GET", direct_path)["items"])
        # Delay independent post-action acquisition by stopping only the
        # Realverax worker. The external Kubernetes operation still proceeds.
        worker.terminate()
        worker.wait(timeout=10)
        apply([{"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": "external-agent-scale", "namespace": NAMESPACE},
            "spec": {"backoffLimit": 0, "template": {"spec": {"serviceAccountName": "external-agent",
                "restartPolicy": "Never", "containers": [{"name": "agent", "image": "direct-agent:e2e",
                    "imagePullPolicy": "Never", "env": [
                        {"name": "TARGET_NAMESPACE", "value": NAMESPACE},
                        {"name": "TARGET_NAME", "value": DEPLOYMENT},
                        {"name": "TARGET_REPLICAS", "value": "5"}]}]}}}}])
        kubectl("-n", NAMESPACE, "wait", "--for=condition=complete", "job/external-agent-scale", "--timeout=90s")
        claim = json.loads(kubectl("-n", NAMESPACE, "logs", "job/external-agent-scale"))
        assert claim["api_status"] == "accepted"
        kubectl("-n", NAMESPACE, "rollout", "status", "deployment/" + DEPLOYMENT, "--timeout=180s", timeout=200)
        actual_after = actual_observation(observer_config)["workloads"][0]
        assert actual_after["desired_replicas"] == actual_after["ready_replicas"] == 5
        audit_after = wait_until("authenticated agent audit delivery", lambda: (v if (v := customer.call("GET", monitor_path))["plan"]["last_audit_at"] != ready["plan"]["last_audit_at"] else None), 60)
        assert audit_after["plan"]["last_audit_at"]
        delayed_reports = customer.call("GET", base + f"/observation-sessions/{session_id}/operational-assessments")["items"]
        assert len(delayed_reports) == len(before_reports)
        assert all(item["canonical_overall_status"] != "VERIFIED" for item in delayed_reports)
        assert len(customer.call("GET", direct_path)["items"]) == direct_before
        worker, worker_log = start_service("assurance-worker-resumed", PRIVATE / "assuranceworker", {
            "OUTCOME_CREDENTIAL_DIR": str(source_dir)})
        processes.append(worker); logs.append(worker_log)
        def accepted_report():
            reports = customer.call("GET", base + f"/observation-sessions/{session_id}/operational-assessments")["items"]
            if not reports:
                return None
            report = reports[-1]
            actions = report.get("observed_actions", [])
            if any(a.get("kubernetes_principal") == agent_principal for a in actions):
                return report
            return None
        report = wait_until("automatic worker assessment of agent patch", accepted_report, 120)
        assert len(customer.call("GET", direct_path)["items"]) > direct_before
        assert report["canonical_overall_status"] == "INCONCLUSIVE"
        initial = report["initial_state"]["observation"]["workloads"][0]
        final = report["independent_final_state"]["observation"]["workloads"][0]
        assert initial["desired_replicas"] == initial["ready_replicas"] == 3
        assert final["desired_replicas"] == final["ready_replicas"] == 5
        assert initial["uid"] == final["uid"] == actual_after["uid"]
        assert any(a.get("kubernetes_principal") == agent_principal and a.get("operation") == "patch" and a.get("origin_authenticated") for a in report["observed_actions"])
        assert not report.get("gateway_preflights") and not report.get("requested_actions")
        assert any(f["code"] == "FORBIDDEN_OPERATION_OBSERVED" and f["status"] == "FAIL" for f in report["findings"])
        assert len(customer.call("GET", base + "/observation-sessions")["items"]) == 1
        findings = customer.call("GET", base + f"/observation-sessions/{session_id}/findings")["items"]
        violation = next(f for f in findings if f["finding"]["code"] == "FORBIDDEN_OPERATION_OBSERVED" and f["report_revision"] == report["revision"])
        assert violation["disposition"] == "violation" and violation["finding"]["evidence"]
        audit_reference = next(ref["reference"] for ref in violation["finding"]["evidence"] if ref["reference"].startswith("audit:"))
        audit_detail = customer.call("GET", base + f"/observation-sessions/{session_id}/evidence-detail?reference=" + urllib.parse.quote(audit_reference))
        assert audit_detail["kind"] == "audit" and audit_detail["digest"]
        direct_id = report["direct_observations"][-1]["id"]
        direct_detail = customer.call("GET", base + f"/observation-sessions/{session_id}/evidence-detail?reference=direct_outcome:{direct_id}")
        assert direct_detail["kind"] == "direct_outcome" and direct_detail["digest"]
        notifications = customer.call("GET", base + "/assurance-notifications")["items"]
        notification = next(n for n in notifications if n["finding_id"] == violation["id"])
        assert notification["delivery_state"] == "pending" and notification["delivery_attempts"] == 0
        monitor = customer.call("GET", monitor_path)
        assert monitor["plan"]["state"] == "active" and monitor["plan"]["last_audit_at"]
        dashboard = customer.call("GET", base + "/assurance-status")
        assert any(c["id"] == contract["id"] for c in dashboard["active_requirements"])
        assert any(s["id"] == session_id and s["latest_revision"] == report["revision"] and s["monitoring_state"] == "active" for s in dashboard["sessions"])
        assert any(s["kind"] == "authenticated_kubernetes_audit" and s["evidence_count"] > 0 for s in dashboard["evidence_sources"])
        # Re-submit the exact persisted API-server event bytes as a controlled
        # retry. The record originated at Kubernetes; no event is fabricated.
        sql = ("SELECT encode(event_raw,'base64') FROM customer_authenticated_audit_events "
               f"WHERE session_id='{session_id}' AND event_payload->'user'->>'username'='{agent_principal}' "
               "ORDER BY acquired_at DESC LIMIT 1")
        raw = base64.b64decode(run("psql", os.environ["DATABASE_URL"], "-Atc", sql))
        assert raw
        report_count, finding_count = len(customer.call("GET", base + f"/observation-sessions/{session_id}/operational-assessments")["items"]), len(findings)
        tls = ssl.create_default_context(cafile=str(PRIVATE / "server/tls.crt"))
        tls.load_cert_chain(str(PRIVATE / "apiserver/client.crt"), str(PRIVATE / "apiserver/client.key"))
        network = json.loads(run("docker", "network", "inspect", "bridge"))
        gateway = next(item["Gateway"] for item in network[0]["IPAM"]["Config"] if "." in item["Gateway"])
        retry = urllib.request.Request(f"https://{gateway}:8443/audit",
            data=b'{"apiVersion":"audit.k8s.io/v1","kind":"EventList","items":[' + raw + b']}',
            headers={"Content-Type": "application/json"}, method="POST")
        retry_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=tls))
        with retry_opener.open(retry, timeout=20) as response:
            assert response.status == 200
        time.sleep(12)
        assert len(customer.call("GET", base + f"/observation-sessions/{session_id}/operational-assessments")["items"]) == report_count
        assert len(customer.call("GET", base + f"/observation-sessions/{session_id}/findings")["items"]) == finding_count
        summary = {"proof": "hosted-kind-customer-monitoring", "namespace": NAMESPACE,
            "workspace_id": workspace, "environment_id": env_id, "session_id": session_id,
            "agent_principal": agent_principal, "observer_principal": observer_principal,
            "before_replicas": 3, "after_replicas": 5, "report_revision": report["revision"],
            "canonical_status": report["canonical_overall_status"], "finding_id": violation["id"],
            "finding_disposition": violation["disposition"], "notification_state": notification["delivery_state"],
            "monitoring_state": monitor["plan"]["state"], "authenticated_audit_at": monitor["plan"]["last_audit_at"],
            "duplicate_replay_tested": True, "delayed_post_action_evidence_tested": True}
        (ARTIFACTS / "result.json").write_text(json.dumps(summary, indent=2) + "\n")
        print(json.dumps(summary))
    finally:
        for process in reversed(processes):
            process.terminate()
        for process in reversed(processes):
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
        for log in logs:
            log.close()


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] not in ("prepare", "run"):
        raise SystemExit("usage: customer-monitoring-acceptance.py prepare|run")
    if sys.argv[1] == "prepare":
        prepare()
    else:
        run_acceptance()
