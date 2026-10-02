package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var containerDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// observeExternalAgentDigest returns the platform-observed container image
// digest of a ready external-agent pod. The value comes from the Kubernetes
// API (pod status container imageID), never from agent input: the external
// agent is not an authority for its own implementation identity.
//
// The digest is normalized from imageID forms like
// "docker://sha256:<hex>" to "sha256:<hex>". Anything else fails closed.
func observeExternalAgentDigest(ctx context.Context, client kubernetes.Interface, namespace, appLabel string) (string, error) {
	if client == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(appLabel) == "" {
		return "", errors.New("external agent identity observation requires a client, namespace, and app label")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pods, err := client.CoreV1().Pods(namespace).List(timeoutCtx, metav1.ListOptions{LabelSelector: "app=" + appLabel})
	if err != nil {
		return "", errors.New("external agent pods unavailable")
	}
	for _, pod := range pods.Items {
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			digest, ok := normalizeContainerDigest(status.ImageID)
			if ok {
				return digest, nil
			}
		}
	}
	return "", errors.New("no ready external agent pod exposes a valid container image digest")
}

func normalizeContainerDigest(imageID string) (string, bool) {
	digest := strings.TrimSpace(strings.TrimPrefix(imageID, "docker://"))
	if !containerDigestPattern.MatchString(digest) {
		return "", false
	}
	return digest, true
}
