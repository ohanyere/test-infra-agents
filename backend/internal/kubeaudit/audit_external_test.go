package kubeaudit_test

import (
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

func TestCallerSuppliedAuditJSONCannotCreateRunnerBoundSource(t *testing.T) {
	data := `{"apiVersion":"audit.k8s.io/v1","kind":"Event","auditID":"fabricated","stage":"ResponseComplete","verb":"patch","requestReceivedTimestamp":"2026-10-17T10:00:00Z","stageTimestamp":"2026-10-17T10:00:01Z","user":{"username":"kubernetes-admin"},"objectRef":{"apiGroup":"apps","apiVersion":"v1","resource":"deployments","namespace":"proof","name":"web"},"responseStatus":{"code":200}}`
	events, err := kubeaudit.Parse(strings.NewReader(data))
	if err != nil || len(events) != 1 {
		t.Fatalf("parse events=%+v err=%v", events, err)
	}
	fabricated := kubeaudit.AcquiredEvent{}
	if fabricated.RunnerBound() {
		t.Fatal("parsed caller-supplied audit JSON acquired a source seal")
	}
	if _, err = kubeaudit.SelectMutation([]kubeaudit.AcquiredEvent{fabricated}, "proof", "web", "patch", "kubernetes-admin"); err == nil {
		t.Fatal("caller-supplied audit JSON was selected as acquired evidence")
	}
}
