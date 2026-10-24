package actionexec

import (
	"context"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestAuthorizeDoesNotMutateAndExecuteRechecksOwnership(t *testing.T) {
	client, _, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	if _, err := client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	action := scale(env.Environment.Namespace, 4)
	result := executor.Authorize(context.Background(), env, action)
	if result.Status != "allowed" || !result.Authorized || result.Executed {
		t.Fatal(result)
	}
	current, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), d.Name, metav1.GetOptions{})
	if *current.Spec.Replicas != 2 {
		t.Fatal("authorization mutated resources")
	}
	current.Labels = nil
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Update(context.Background(), current, metav1.UpdateOptions{})
	result = executor.ExecuteAction(context.Background(), env, action)
	if result.Code != "unmanaged_resource" || result.Executed {
		t.Fatal(result)
	}
}
func TestStructuredDenialCodes(t *testing.T) {
	_, _, env, executor := setup(t)
	tests := []struct {
		action agentgateway.ProposedAction
		code   string
	}{
		{agentgateway.ProposedAction{Type: "shell"}, "unsupported_action"},
		{agentgateway.ProposedAction{Type: "teleport_workload_image", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "eval-reference-app"}}, "unsupported_action"},
		{scale("production", 4), "cross_namespace"},
		{scale(env.Environment.Namespace, 0), "invalid_action_parameters"},
		{agentgateway.ProposedAction{Type: "scale_workload", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "another-app"}}, "disallowed_resource"},
	}
	for _, test := range tests {
		result := executor.Authorize(context.Background(), env, test.action)
		if result.Code != test.code || result.Executed || result.Authorized {
			t.Fatalf("want %s got %+v", test.code, result)
		}
	}
}
func TestInspectAndNoOpNeverMutate(t *testing.T) {
	client, _, env, executor := setup(t)
	client.ClearActions()
	for _, kind := range []string{"inspect", "no_op"} {
		result := executor.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: kind})
		if !result.Executed {
			t.Fatal(result)
		}
	}
	for _, call := range client.Actions() {
		if call.GetVerb() != "get" {
			t.Fatalf("unexpected mutation: %s", call.GetVerb())
		}
	}
}
