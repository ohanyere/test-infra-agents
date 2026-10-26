// directkubeagent is a deterministic external process for Kind qualification.
// It uses only its mounted Kubernetes ServiceAccount and has no Realverax API.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type report struct {
	Target         string `json:"target"`
	ClaimedSuccess bool   `json:"claimed_success"`
	Attempted      bool   `json:"attempted"`
	APIStatus      string `json:"api_status"`
}

func main() {
	namespace, name := os.Getenv("TARGET_NAMESPACE"), os.Getenv("TARGET_NAME")
	replicas, err := strconv.ParseInt(os.Getenv("TARGET_REPLICAS"), 10, 32)
	if namespace == "" || name == "" || err != nil || replicas < 1 {
		fmt.Fprintln(os.Stderr, "target namespace, name, and positive replica count are required")
		os.Exit(2)
	}
	out := report{Target: namespace + "/" + name, ClaimedSuccess: os.Getenv("CLAIM_SUCCESS") == "true"}
	if os.Getenv("CLAIM_ONLY") == "true" {
		out.APIStatus = "not_attempted"
	} else {
		config, configErr := rest.InClusterConfig()
		if configErr != nil {
			fmt.Fprintln(os.Stderr, "Kubernetes ServiceAccount configuration unavailable")
			os.Exit(2)
		}
		client, clientErr := kubernetes.NewForConfig(config)
		if clientErr != nil {
			fmt.Fprintln(os.Stderr, "Kubernetes client construction failed")
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
		out.Attempted = true
		_, err = client.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			out.APIStatus = "denied_or_failed"
		} else {
			out.APIStatus = "accepted"
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}
