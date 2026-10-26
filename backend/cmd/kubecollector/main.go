// kubecollector is a customer-hosted, read-only Kubernetes evidence sender.
// It never executes actions and fails closed when collection is unavailable.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const maxResources = 2000

type collector struct {
	client                        kubernetes.Interface
	http                          *http.Client
	apiURL, sessionID, credential string
	namespaces                    []string
	sequence                      int64
}

func main() {
	if err := run(); err != nil {
		slog.Error("collector stopped", "reason", safeError(err))
		os.Exit(1)
	}
}
func run() error {
	apiURL := flag.String("api", os.Getenv("REALVERAX_API_URL"), "Realverax API base URL")
	session := flag.String("session", os.Getenv("REALVERAX_OBSERVATION_SESSION_ID"), "observation session UUID")
	token := flag.String("token", os.Getenv("REALVERAX_COLLECTOR_TOKEN"), "one-time provisioned collector credential")
	namespaceCSV := flag.String("namespaces", os.Getenv("REALVERAX_NAMESPACES"), "comma-separated authorized namespaces")
	interval := flag.Duration("interval", 30*time.Second, "bounded collection interval")
	duration := flag.Duration("duration", 10*time.Minute, "observation window before final evidence")
	flag.Parse()
	namespaces := split(*namespaceCSV)
	if err := validateConfig(*apiURL, *session, *token, namespaces, *interval, *duration); err != nil {
		return err
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("in-cluster Kubernetes service account credentials are unavailable")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("initialize Kubernetes read-only client")
	}
	httpClient, err := apiHTTPClient(*apiURL)
	if err != nil {
		return err
	}
	c := &collector{client: client, http: httpClient, apiURL: strings.TrimRight(*apiURL, "/"), sessionID: *session, credential: *token, namespaces: namespaces}
	started := time.Now()
	if err = c.send("baseline"); err != nil {
		return fmt.Errorf("baseline collection failed: %w", err)
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	stabilityCount := 0
	for {
		remaining := time.Until(started.Add(*duration))
		if remaining <= 0 {
			break
		}
		wait := *interval
		if remaining < wait {
			wait = remaining
		}
		select {
		case <-ticker.C:
			if time.Since(started) >= *duration {
				break
			}
			if err = c.send("stability"); err != nil {
				return fmt.Errorf("stability collection failed: %w", err)
			}
			stabilityCount++
		case <-time.After(wait):
		}
		if time.Until(started.Add(*duration)) <= 0 {
			break
		}
	}
	if stabilityCount < 2 {
		return errors.New("observation window ended before two stability samples were collected")
	}
	return c.send("final")
}

func (c *collector) send(phase string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC()
	obs := agentgateway.Observation{Timestamp: now, Workloads: []agentgateway.WorkloadObservation{}, Pods: []agentgateway.PodObservation{}, Autoscalers: []agentgateway.AutoscalerObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}
	for _, namespace := range c.namespaces {
		deployments, err := c.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{Limit: 1000})
		if err != nil {
			return errors.New("Kubernetes Deployment read failed")
		}
		if deployments.Continue != "" {
			return errors.New("Kubernetes Deployment result exceeded one bounded page")
		}
		for i := range deployments.Items {
			d := &deployments.Items[i]
			replicas := int32(1)
			if d.Spec.Replicas != nil {
				replicas = *d.Spec.Replicas
			}
			w := agentgateway.WorkloadObservation{Kind: "Deployment", Name: d.Name, Namespace: d.Namespace, DesiredReplicas: replicas, ReadyReplicas: d.Status.ReadyReplicas, AvailableReplicas: d.Status.AvailableReplicas, UpdatedReplicas: d.Status.UpdatedReplicas, UID: string(d.UID), ResourceVersion: d.ResourceVersion, Generation: d.Generation}
			if len(d.Spec.Template.Spec.Containers) > 0 {
				w.Image = d.Spec.Template.Spec.Containers[0].Image
			}
			obs.Workloads = append(obs.Workloads, w)
		}
		pods, err := c.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1000})
		if err != nil {
			return errors.New("Kubernetes Pod read failed")
		}
		if pods.Continue != "" {
			return errors.New("Kubernetes Pod result exceeded one bounded page")
		}
		for i := range pods.Items {
			p := &pods.Items[i]
			item := agentgateway.PodObservation{Name: p.Name, Namespace: p.Namespace, Phase: string(p.Status.Phase)}
			for _, status := range p.Status.ContainerStatuses {
				item.RestartCount += status.RestartCount
			}
			for _, condition := range p.Status.Conditions {
				if condition.Type == "Ready" {
					item.Ready = condition.Status == "True"
					break
				}
			}
			for _, owner := range p.OwnerReferences {
				if owner.Controller != nil && *owner.Controller {
					item.OwningWorkload = owner.Name
					break
				}
			}
			obs.Pods = append(obs.Pods, item)
		}
		if len(obs.Workloads)+len(obs.Pods) > maxResources {
			return errors.New("Kubernetes resource scope exceeds collector limit")
		}
	}
	c.sequence++
	body := map[string]any{"batch_id": newUUID(), "sequence": c.sequence, "schema_version": "customer-kubernetes-observation:v1", "phase": phase, "captured_at": now, "observation": obs}
	encoded, err := json.Marshal(body)
	if err != nil {
		return errors.New("encode collector evidence")
	}
	endpoint := c.apiURL + "/v1/collector/observation-sessions/" + url.PathEscape(c.sessionID) + "/evidence"
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
		if err != nil {
			return errors.New("build evidence request")
		}
		req.Header.Set("Authorization", "Bearer "+c.credential)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			if resp.StatusCode < 500 {
				return fmt.Errorf("evidence API rejected batch with status %d", resp.StatusCode)
			}
			lastErr = fmt.Errorf("evidence API returned status %d", resp.StatusCode)
		} else {
			lastErr = errors.New("evidence API request failed")
		}
		delay := time.Duration(1<<attempt) * time.Second
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return errors.New("evidence API retry window expired")
		}
	}
	return lastErr
}

func validateConfig(api, session, token string, namespaces []string, interval, duration time.Duration) error {
	u, err := url.Parse(api)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
		return errors.New("API URL must use HTTPS (HTTP is allowed only for loopback development)")
	}
	if session == "" || !strings.HasPrefix(token, "rvxc_") || len(token) != 48 {
		return errors.New("session ID and collector credential are required")
	}
	if len(namespaces) == 0 || len(namespaces) > 100 || interval < time.Second || interval > 5*time.Minute || duration < 3*interval || duration > 24*time.Hour {
		return errors.New("invalid namespace scope, interval, or observation duration")
	}
	for _, ns := range namespaces {
		if ns == "" || len(ns) > 63 {
			return errors.New("invalid namespace scope")
		}
	}
	return nil
}
func apiHTTPClient(raw string) (*http.Client, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid API URL")
	}
	if u.Scheme == "https" {
		return agentgateway.NewPolicyHTTPClient(agentgateway.EndpointPolicy{}, 15*time.Second), nil
	}
	if u.Scheme != "http" || !isLoopback(u.Hostname()) {
		return nil, errors.New("cleartext API is permitted only on loopback")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("collector API redirects are disabled") }
	return client, nil
}
func isLoopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func split(value string) []string {
	out := []string{}
	for _, v := range strings.Split(value, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
func newUUID() string { return uuid.NewString() }
func safeError(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}
