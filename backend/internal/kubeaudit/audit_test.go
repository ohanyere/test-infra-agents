package kubeaudit

import (
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
