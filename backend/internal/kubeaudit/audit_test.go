package kubeaudit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const validEvent = `{"apiVersion":"audit.k8s.io/v1","kind":"Event","auditID":"audit-1","stage":"ResponseComplete","verb":"patch","requestURI":"/apis/apps/v1/namespaces/proof/deployments/web","requestReceivedTimestamp":"2026-10-17T10:00:00Z","stageTimestamp":"2026-10-17T10:00:01Z","user":{"username":"kubernetes-admin"},"objectRef":{"apiGroup":"apps","apiVersion":"v1","resource":"deployments","namespace":"proof","name":"web","uid":"uid-1"},"responseStatus":{"code":200,"reason":"OK"}}`

func TestParseKubernetesAuditResponseEvent(t *testing.T) {
	events, err := Parse(strings.NewReader(validEvent + "\n"))
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	event := events[0]
	if event.Verb != "patch" || event.ObjectRef.Resource != "deployments" || event.ObjectRef.Namespace != "proof" || event.ObjectRef.Name != "web" || event.User.Username != "kubernetes-admin" || event.Response.Code != 200 || event.AuditID != "audit-1" {
		t.Fatalf("event fields lost: %+v", event)
	}
}

func TestParseRejectsMalformedUnsupportedIncompleteAndOversizedEvents(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"malformed", "{"},
		{"truncated", validEvent[:len(validEvent)-10]},
		{"unsupported version", strings.Replace(validEvent, AuditAPIVersion, "audit.k8s.io/v9", 1)},
		{"missing resource", strings.Replace(validEvent, `"resource":"deployments",`, "", 1)},
		{"missing timestamp", strings.Replace(validEvent, `"requestReceivedTimestamp":"2026-10-17T10:00:00Z",`, "", 1)},
		{"oversized", strings.Repeat("x", maxRecordSize+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(test.data)); err == nil {
				t.Fatal("invalid audit record accepted")
			}
		})
	}
}

func TestAcquireKindRejectsCallerOutsideHostedRunner(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_RUN_ID", "")
	if _, _, err := AcquireHostedKind(t.Context()); err == nil {
		t.Fatal("source acquisition allowed outside the hosted runner boundary")
	}
}

func TestSelectMutationRequiresSourceBindingAndExactEventIdentity(t *testing.T) {
	events, err := Parse(strings.NewReader(validEvent))
	if err != nil {
		t.Fatal(err)
	}
	identity := SourceIdentity{ClusterIdentity: "kind:evalplane-e2e", ConfigurationIdentity: configurationIdentity(), VerificationState: "runner_bound_test_source"}
	acquired := []AcquiredEvent{newAcquiredEvent(events[0], identity)}
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err != nil {
		t.Fatalf("valid acquired event rejected: %v", err)
	}
	for _, test := range []struct{ namespace, name, principal string }{{"other", "web", "kubernetes-admin"}, {"proof", "other", "kubernetes-admin"}, {"proof", "web", "other"}} {
		if _, err = SelectMutation(acquired, test.namespace, test.name, "patch", test.principal); err == nil {
			t.Fatalf("mismatched event accepted: %+v", test)
		}
	}
	acquired[0].source.ClusterIdentity = "kind:another-cluster"
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("wrong cluster binding accepted")
	}
	acquired[0].source.ClusterIdentity = "kind:evalplane-e2e"
	acquired[0].source.ConfigurationIdentity = "caller-config"
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("wrong source configuration accepted")
	}
	acquired[0] = newAcquiredEvent(events[0], identity)
	acquired[0].event.Response.Code = 403
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("failed API operation accepted as successful mutation")
	}
}

func TestSelectResponseRetainsRBACDenialWithoutCallingItMutation(t *testing.T) {
	events, err := Parse(strings.NewReader(strings.Replace(validEvent, `"code":200`, `"code":403`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	source := SourceIdentity{ClusterIdentity: "kind:evalplane-e2e", ConfigurationIdentity: configurationIdentity(), VerificationState: "runner_bound_test_source"}
	acquired := []AcquiredEvent{newAcquiredEvent(events[0], source)}
	selected, err := SelectResponse(acquired, "proof", "web", "patch", "kubernetes-admin")
	if err != nil {
		t.Fatal(err)
	}
	event, _, ok := selected.Snapshot()
	if !ok || event.Response.Code != 403 {
		t.Fatal("RBAC denial was not preserved as authenticated response evidence")
	}
	if _, err := SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("denied request became a successful mutation")
	}
	if _, err := SelectResponse(acquired, "proof", "web", "patch", "another-principal"); err == nil {
		t.Fatal("wrong principal matched audit response")
	}
}

func TestAcquiredEventSealBindsEventAndSourceAndSnapshotsAreDefensive(t *testing.T) {
	events, err := Parse(strings.NewReader(strings.Replace(validEvent, `"username":"kubernetes-admin"`, `"username":"kubernetes-admin","groups":["system:authenticated"]`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	source := SourceIdentity{ClusterIdentity: "kind:evalplane-e2e", ConfigurationIdentity: configurationIdentity(), VerificationState: "runner_bound_test_source", ProvenanceLimitations: []string{"runner controlled"}}
	acquired := newAcquiredEvent(events[0], source)
	if !acquired.RunnerBound() {
		t.Fatal("genuine sealed event rejected")
	}
	eventCopy, sourceCopy, ok := acquired.Snapshot()
	if !ok {
		t.Fatal("valid event snapshot rejected")
	}
	eventCopy.User.Groups[0] = "forged"
	sourceCopy.ProvenanceLimitations[0] = "forged"
	eventCopy.Response.Code = 403
	sourceCopy.ClusterIdentity = "kind:other"
	if !acquired.RunnerBound() {
		t.Fatal("mutating defensive snapshots changed sealed state")
	}
	if gotEvent, gotSource, ok := acquired.Snapshot(); !ok || gotEvent.Response.Code != 200 || gotEvent.User.Groups[0] != "system:authenticated" || gotSource.ProvenanceLimitations[0] != "runner controlled" {
		t.Fatalf("snapshot state changed: event=%+v source=%+v valid=%v", gotEvent, gotSource, ok)
	}

	mutated := acquired
	mutated.event.User.Groups[0] = "forged"
	if mutated.RunnerBound() {
		t.Fatal("nested event mutation retained source authority")
	}
	mutated = acquired
	mutated.source.ProvenanceLimitations[0] = "forged"
	if mutated.RunnerBound() {
		t.Fatal("nested source mutation retained source authority")
	}
}

func TestParseIgnoresNonResponseStageAndRejectsDuplicateStage(t *testing.T) {
	request := strings.Replace(validEvent, `"stage":"ResponseComplete"`, `"stage":"RequestReceived"`, 1)
	events, err := Parse(strings.NewReader(request + "\n" + validEvent))
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if _, err := Parse(strings.NewReader(validEvent + "\n" + validEvent)); err == nil {
		t.Fatal("duplicate audit stage accepted")
	}
}

func TestParsePreservesFailedResponsesAndMissingPrincipal(t *testing.T) {
	failed := strings.Replace(validEvent, `"code":200`, `"code":403`, 1)
	noUser := strings.Replace(validEvent, `"user":{"username":"kubernetes-admin"},`, `"user":{},`, 1)
	noUser = strings.Replace(noUser, `"auditID":"audit-1"`, `"auditID":"audit-2"`, 1)
	events, err := Parse(strings.NewReader(failed + "\n" + noUser))
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].Response.Code != 403 || events[1].User.Username != "" {
		t.Fatalf("failure or absent principal not preserved: %+v", events)
	}
	identity := SourceIdentity{ClusterIdentity: "kind:evalplane-e2e", ConfigurationIdentity: configurationIdentity(), VerificationState: "runner_bound_test_source"}
	if _, err = SelectMutation([]AcquiredEvent{newAcquiredEvent(events[1], identity)}, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("event missing authenticated principal matched expected actor")
	}
	if _, err = SelectMutation([]AcquiredEvent{{}}, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("caller-constructed event without acquisition seal was accepted")
	}
}

func TestSourceIdentityIsExplicitlyRunnerBoundAndNotCryptographic(t *testing.T) {
	identity := SourceIdentity{SourceType: "kubernetes-apiserver-audit", ClusterIdentity: "kind:evalplane-e2e", VerificationState: "runner_bound_test_source", AcquiredAt: time.Now(), ProvenanceLimitations: []string{"not cryptographic"}}
	if identity.ClusterIdentity != "kind:evalplane-e2e" || identity.VerificationState != "runner_bound_test_source" || len(identity.ProvenanceLimitations) == 0 {
		t.Fatalf("source boundary not explicit: %+v", identity)
	}
}

const validAuditManifest = `apiVersion: v1
kind: Pod
spec:
  containers:
    - name: kube-apiserver
      command: [kube-apiserver, --audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml, --audit-log-path=/var/log/kubernetes/audit/audit.log, --audit-log-mode=blocking, --audit-log-maxage=1, --audit-log-maxbackup=2, --audit-log-maxsize=20]
      volumeMounts:
        - {name: audit-policies, mountPath: /etc/kubernetes/policies/audit-policy.yaml, readOnly: true}
        - {name: audit-logs, mountPath: /var/log/kubernetes/audit, readOnly: false}
  volumes:
    - {name: audit-policies, hostPath: {path: /etc/kubernetes/policies/audit-policy.yaml, type: File}}
    - {name: audit-logs, hostPath: {path: /var/log/kubernetes/audit, type: DirectoryOrCreate}}
`

func TestValidateAuditArgumentsSupportsManifestArgumentEncodings(t *testing.T) {
	manifests := []string{
		validAuditManifest,
		`apiVersion: v1
kind: Pod
spec:
  containers:
    - name: kube-apiserver
      command:
        - kube-apiserver
        - --audit-log-path
        - /var/log/kubernetes/audit/audit.log
        - --audit-policy-file
        - /etc/kubernetes/policies/audit-policy.yaml
        - --audit-log-mode=blocking
        - --audit-log-maxage=1
        - --audit-log-maxbackup=2
        - --audit-log-maxsize=20
      volumeMounts:
        - {name: audit-policies, mountPath: /etc/kubernetes/policies/audit-policy.yaml, readOnly: true}
        - {name: audit-logs, mountPath: /var/log/kubernetes/audit, readOnly: false}
  volumes:
    - {name: audit-policies, hostPath: {path: /etc/kubernetes/policies/audit-policy.yaml, type: File}}
    - {name: audit-logs, hostPath: {path: /var/log/kubernetes/audit, type: DirectoryOrCreate}}
`,
	}
	for _, manifest := range manifests {
		if err := validateAuditArguments([]byte(manifest)); err != nil {
			t.Fatalf("valid kube-apiserver arguments rejected: %v", err)
		}
	}
}

func TestAuditArgumentDiagnosticsNameOnlyTheMismatchedFlags(t *testing.T) {
	err := validateAuditArgumentValues([]string{"kube-apiserver", "--audit-policy-file=/sensitive/wrong-path", "--unrelated-token=secret"})
	if err == nil {
		t.Fatal("missing and mismatched audit arguments accepted")
	}
	message := err.Error()
	for _, want := range []string{"audit-policy-file mismatched", "audit-log-path missing"} {
		if !strings.Contains(message, want) {
			t.Fatalf("diagnostic %q missing from %q", want, message)
		}
	}
	if strings.Contains(message, "sensitive") || strings.Contains(message, "secret") {
		t.Fatalf("diagnostic exposed raw arguments: %q", message)
	}
}

func TestValidateAuditArgumentsRequiresTheNamedAPIServerContainer(t *testing.T) {
	manifest := []byte(`apiVersion: v1
kind: Pod
spec:
  containers:
    - name: unrelated
      command:
        - kube-apiserver
        - --audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml
        - --audit-log-path=/var/log/kubernetes/audit/audit.log
`)
	if err := validateAuditArguments(manifest); err == nil || !strings.Contains(err.Error(), "kube-apiserver container arguments are missing") {
		t.Fatalf("expected exact API-server container selection failure, got %v", err)
	}
}

func TestValidateAuditArgumentsReportsBothMissingFlags(t *testing.T) {
	manifest := []byte(`apiVersion: v1
kind: Pod
spec:
  containers:
    - name: kube-apiserver
      command: [kube-apiserver]
`)
	err := validateAuditArguments(manifest)
	if err == nil {
		t.Fatal("manifest without audit flags accepted")
	}
	for _, flag := range []string{"audit-policy-file missing", "audit-log-path missing", "audit-log-mode missing", "audit-log-maxage missing", "audit-log-maxbackup missing", "audit-log-maxsize missing"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("expected diagnostic for %q, got %v", flag, err)
		}
	}
}

func TestValidateAuditArgumentsRejectsWritablePolicyMount(t *testing.T) {
	manifest := strings.ReplaceAll(validAuditManifest, "mountPath: /etc/kubernetes/policies/audit-policy.yaml, readOnly: true", "mountPath: /etc/kubernetes/policies/audit-policy.yaml, readOnly: false")
	if err := validateAuditArguments([]byte(manifest)); err == nil || !strings.Contains(err.Error(), "audit-policies audit volume mount missing or mismatched") {
		t.Fatalf("writable audit policy mount was not rejected: %v", err)
	}
}

func TestAuditArgumentDiagnosticsRejectDuplicateFlags(t *testing.T) {
	err := validateAuditArgumentValues([]string{
		"kube-apiserver",
		"--audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml",
		"--audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml",
		"--audit-log-path=/var/log/kubernetes/audit/audit.log",
	})
	if err == nil || !strings.Contains(err.Error(), "audit-policy-file duplicated") {
		t.Fatalf("expected duplicate flag diagnostic, got %v", err)
	}
}

func TestRunningProcessArgumentsReadsSelectedContainerRuntimeSpec(t *testing.T) {
	inspection, err := json.Marshal(map[string]any{"info": map[string]any{"runtimeSpec": map[string]any{
		"process": map[string]any{"args": []string{"kube-apiserver", "--audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml", "--audit-log-path=/var/log/kubernetes/audit/audit.log", "--audit-log-mode=blocking", "--audit-log-maxage=1", "--audit-log-maxbackup=2", "--audit-log-maxsize=20"}},
		"mounts": []map[string]any{
			{"destination": "/etc/kubernetes/policies/audit-policy.yaml", "options": []string{"rbind", "ro"}},
			{"destination": "/var/log/kubernetes/audit", "options": []string{"rbind", "rw"}},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := runningProcessArguments(inspection)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAuditArgumentValues(arguments); err != nil {
		t.Fatalf("running container arguments rejected: %v", err)
	}
	if err = validateRuntimeAuditMounts(inspection); err != nil {
		t.Fatalf("running container mounts rejected: %v", err)
	}
}

func TestRuntimeAuditMountsRequireReadonlyPolicyAndWritableLogDirectory(t *testing.T) {
	inspection := []byte(`{"info":{"runtimeSpec":{"mounts":[{"destination":"/etc/kubernetes/policies/audit-policy.yaml","options":["rw"]},{"destination":"/var/log/kubernetes/audit","options":["rw"]}]}}}`)
	if err := validateRuntimeAuditMounts(inspection); err == nil || !strings.Contains(err.Error(), "audit policy runtime mount missing or has incorrect access mode") {
		t.Fatalf("writable policy runtime mount was not rejected: %v", err)
	}
}
