package main

import (
	"context"
	"errors"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestStage3ContradictionReturnsPlausibleValueAfterTwoExternalMutations(t *testing.T) {
	env := convergenceTestEnvironment()
	namespace := env.Environment.Namespace
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: scenariorunner.ReferenceDeploymentName, Namespace: namespace}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v17"}}}}}}
	client := fake.NewSimpleClientset(deployment)
	delegate := fixedActions{result: actionexec.ActionResult{ActionType: "set_live_workload_image", Authorized: true, Executed: true, Status: "executed"}}
	actions := stage3ContradictionActions{delegate: delegate, client: client}
	target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: namespace, Name: scenariorunner.ReferenceDeploymentName}
	result := actions.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: "set_live_workload_image", Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	if !result.Executed || result.Status != "executed" {
		t.Fatalf("delegate result changed: %+v", result)
	}
	got, err := client.AppsV1().Deployments(namespace).Get(context.Background(), deployment.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.Template.Spec.Containers[0].Image != "evalplane-reference:v17" {
		t.Fatalf("final plausible value=%s", got.Spec.Template.Spec.Containers[0].Image)
	}
	updates := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "deployments" {
			updates++
		}
	}
	if updates != 2 {
		t.Fatalf("out-of-band update count=%d", updates)
	}
}

func TestStage3ContradictionPreservesFixtureUpdateFailure(t *testing.T) {
	env := convergenceTestEnvironment()
	namespace := env.Environment.Namespace
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: scenariorunner.ReferenceDeploymentName, Namespace: namespace}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v17"}}}}}}
	client := fake.NewSimpleClientset(deployment)
	conflict := apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, deployment.Name, errors.New("resource version changed"))
	client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, conflict })
	delegate := fixedActions{result: actionexec.ActionResult{ActionType: "set_live_workload_image", Authorized: true, Executed: true, Status: "executed"}}
	actions := stage3ContradictionActions{delegate: delegate, client: client}
	target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: namespace, Name: deployment.Name}
	result := actions.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: "set_live_workload_image", Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	if result.Status != "failed" || result.Executed || result.Cause == nil || !apierrors.IsConflict(result.Cause) {
		t.Fatalf("fixture conflict was lost or treated as success: %+v", result)
	}
}

func TestStage3ContradictionIgnoresDesiredStateActions(t *testing.T) {
	env := convergenceTestEnvironment()
	namespace := env.Environment.Namespace
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: scenariorunner.ReferenceDeploymentName, Namespace: namespace}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v17"}}}}}}
	client := fake.NewSimpleClientset(deployment)
	delegate := fixedActions{result: actionexec.ActionResult{ActionType: "set_desired_workload_image", Authorized: true, Executed: true, Status: "executed"}}
	actions := stage3ContradictionActions{delegate: delegate, client: client}
	target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: namespace, Name: scenariorunner.ReferenceDeploymentName}
	result := actions.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	if !result.Executed || result.Status != "executed" {
		t.Fatalf("delegate result changed: %+v", result)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "deployments" {
			t.Fatal("desired-state action triggered live out-of-band mutations")
		}
	}
}
