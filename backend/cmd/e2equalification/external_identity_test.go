package main

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func externalAgentPod(app, imageID string, ready bool) *corev1.Pod {
	condition := corev1.ConditionFalse
	if ready {
		condition = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: app + "-pod", Namespace: "evalplane-e2e-harness", Labels: map[string]string{"app": app}},
		Status: corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: condition}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: app, ImageID: imageID}},
		},
	}
}

func TestObserveExternalAgentDigestRequiresReadyPodWithValidDigest(t *testing.T) {
	valid := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	client := kubernetesfake.NewSimpleClientset(externalAgentPod("external-agent", "docker://"+valid, true))
	got, err := observeExternalAgentDigest(context.Background(), client, "evalplane-e2e-harness", "external-agent")
	if err != nil || got != valid {
		t.Fatalf("digest=%q err=%v", got, err)
	}
}

func TestObserveExternalAgentDigestFailsClosed(t *testing.T) {
	valid := "docker://sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name  string
		pods  []*corev1.Pod
		label string
	}{
		{"no pods", nil, "external-agent"},
		{"not ready", []*corev1.Pod{externalAgentPod("external-agent", valid, false)}, "external-agent"},
		{"malformed digest", []*corev1.Pod{externalAgentPod("external-agent", "docker://sha256:zzz", true)}, "external-agent"},
		{"tag instead of digest", []*corev1.Pod{externalAgentPod("external-agent", "external-agent:e2e", true)}, "external-agent"},
		{"wrong app label", []*corev1.Pod{externalAgentPod("external-agent", valid, true)}, "other-agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			for _, pod := range tc.pods {
				if _, err := client.CoreV1().Pods("evalplane-e2e-harness").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := observeExternalAgentDigest(context.Background(), client, "evalplane-e2e-harness", tc.label); err == nil {
				t.Fatal("unobserved identity was accepted")
			}
		})
	}
}
