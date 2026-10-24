package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/operational"
)

// consultGuidance is opt-in for the deterministic reference fixture. The
// guidance credential is separate from its execution identity. A returned
// requirement can lead this fixture to elect no-op; Realverax did not enforce
// or authorize the Kubernetes operation.
func consultGuidance(ctx context.Context, req agentgateway.Request, response agentgateway.Response) (agentgateway.Response, error) {
	endpoint, token := os.Getenv("REALVERAX_GUIDANCE_ENDPOINT"), os.Getenv("REALVERAX_GUIDANCE_TOKEN")
	if endpoint == "" && token == "" {
		return response, nil
	}
	if endpoint == "" || token == "" {
		return response, errors.New("incomplete guidance configuration")
	}
	if len(response.Actions) == 0 || response.Actions[0].Target == nil {
		return response, nil
	}
	action := response.Actions[0]
	operation := ""
	switch action.Type {
	case "set_live_workload_image", "set_desired_workload_image", "set_workload_image":
		operation = "set_image"
	case "scale_workload":
		operation = "scale"
	case "delete_workload":
		operation = "delete"
	case "patch_workload":
		operation = "patch"
	default:
		return response, nil
	}
	target := action.Target
	proposed := operational.ProposedOperation{RunID: req.RunID, SessionID: req.SessionID, Namespace: target.Namespace, ResourceKind: target.Kind, ResourceName: target.Name, Operation: operation, Service: target.Namespace + "/" + target.Name}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return response, err
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return response, errors.New("guidance endpoint requires HTTPS or loopback HTTP")
		}
	}
	body, err := json.Marshal(proposed)
	if err != nil {
		return response, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return response, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	result, err := client.Do(request)
	if err != nil {
		return response, err
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		return response, fmt.Errorf("guidance request rejected with status %d", result.StatusCode)
	}
	var guidance operational.Guidance
	decoder := json.NewDecoder(io.LimitReader(result.Body, 1<<20))
	if err = decoder.Decode(&guidance); err != nil {
		return response, err
	}
	if guidance.ID == "" || guidance.AgentVersionID != req.AgentVersionID || guidance.ProposedOperation.Namespace != target.Namespace || guidance.ProposedOperation.ResourceName != target.Name {
		return response, errors.New("guidance response scope mismatch")
	}
	response.ReasoningSummary = "consulted Realverax guidance " + guidance.ID
	for _, contract := range guidance.Requirements {
		for _, rule := range contract.Rules {
			matches := rule.Namespace == "" || rule.Namespace == target.Namespace
			matches = matches && (rule.ResourceKind == "" || rule.ResourceKind == target.Kind) && (rule.ResourceName == "" || rule.ResourceName == target.Name)
			forbidden := rule.Type == "forbid_operation" && (rule.Operation == operation || rule.Operation == "patch" && (operation == "set_image" || operation == "scale"))
			if matches && (forbidden || (rule.Type == "require_approval" && rule.ApprovalRequired)) {
				response.Actions = []agentgateway.ProposedAction{{Type: "no_op", Parameters: map[string]any{}}}
				response.ReasoningSummary = "reference fixture elected no-op after guidance " + guidance.ID
				return response, nil
			}
		}
	}
	if len(guidance.Requirements) == 0 {
		response.ReasoningSummary += "; no active requirements returned"
	}
	if !guidance.TrustworthyHistoryAvailable {
		response.ReasoningSummary += "; no eligible matching history"
	}
	response.ReasoningSummary = strings.TrimSpace(response.ReasoningSummary)
	return response, nil
}
