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
	acquired := []AcquiredEvent{{Event: events[0], Source: identity, sealed: true}}
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err != nil {
		t.Fatalf("valid acquired event rejected: %v", err)
	}
	for _, test := range []struct{ namespace, name, principal string }{{"other", "web", "kubernetes-admin"}, {"proof", "other", "kubernetes-admin"}, {"proof", "web", "other"}} {
		if _, err = SelectMutation(acquired, test.namespace, test.name, "patch", test.principal); err == nil {
			t.Fatalf("mismatched event accepted: %+v", test)
		}
	}
	acquired[0].Source.ClusterIdentity = "kind:another-cluster"
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("wrong cluster binding accepted")
	}
	acquired[0].Source.ClusterIdentity = "kind:evalplane-e2e"
	acquired[0].Source.ConfigurationIdentity = "caller-config"
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("wrong source configuration accepted")
	}
	acquired[0].Source.ConfigurationIdentity = configurationIdentity()
	acquired[0].Event.Response.Code = 403
	if _, err = SelectMutation(acquired, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("failed API operation accepted as successful mutation")
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
	events, err := Parse(strings.NewReader(failed + "\n" + noUser))
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].Response.Code != 403 || events[1].User.Username != "" {
		t.Fatalf("failure or absent principal not preserved: %+v", events)
	}
	identity := SourceIdentity{ClusterIdentity: "kind:evalplane-e2e", ConfigurationIdentity: configurationIdentity(), VerificationState: "runner_bound_test_source"}
	if _, err = SelectMutation([]AcquiredEvent{{Event: events[1], Source: identity, sealed: true}}, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("event missing authenticated principal matched expected actor")
	}
	if _, err = SelectMutation([]AcquiredEvent{{Event: events[0], Source: identity}}, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("caller-constructed event without acquisition seal was accepted")
	}
}

func TestSourceIdentityIsExplicitlyRunnerBoundAndNotCryptographic(t *testing.T) {
	identity := SourceIdentity{SourceType: "kubernetes-apiserver-audit", ClusterIdentity: "kind:evalplane-e2e", VerificationState: "runner_bound_test_source", AcquiredAt: time.Now(), ProvenanceLimitations: []string{"not cryptographic"}}
	if identity.ClusterIdentity != "kind:evalplane-e2e" || identity.VerificationState != "runner_bound_test_source" || len(identity.ProvenanceLimitations) == 0 {
		t.Fatalf("source boundary not explicit: %+v", identity)
	}
}

func TestValidateAuditArgumentsSupportsManifestArgumentEncodings(t *testing.T) {
	manifests := []string{
		`apiVersion: v1
kind: Pod
spec:
  containers:
    - name: kube-apiserver
      command: [kube-apiserver, --audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml, --audit-log-path=/var/log/kubernetes/audit/audit.log]
`,
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
	inspection, err := json.Marshal(map[string]any{"info": map[string]any{"runtimeSpec": map[string]any{"process": map[string]any{"args": []string{"kube-apiserver", "--audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml", "--audit-log-path=/var/log/kubernetes/audit/audit.log"}}}}})
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
}
