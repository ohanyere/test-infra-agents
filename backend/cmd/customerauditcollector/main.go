// customerauditcollector forwards a customer-operated audit export. It has no
// Kubernetes write permission and does not attest the API server's identity.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

func main() {
	path := os.Getenv("AUDIT_LOG_PATH")
	api := os.Getenv("REALVERAX_API_URL")
	session := os.Getenv("OBSERVATION_SESSION_ID")
	cluster := os.Getenv("REGISTERED_CLUSTER_ID")
	token := os.Getenv("COLLECTOR_CREDENTIAL")
	if path == "" || api == "" || session == "" || cluster == "" || token == "" {
		log.Fatal("AUDIT_LOG_PATH, REALVERAX_API_URL, OBSERVATION_SESSION_ID, REGISTERED_CLUSTER_ID, and COLLECTOR_CREDENTIAL are required")
	}
	parsed, err := url.Parse(api)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		log.Fatal("REALVERAX_API_URL must be an HTTPS origin")
	}
	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	client := &http.Client{Timeout: 15 * time.Second}
	endpoint := strings.TrimRight(api, "/") + "/v1/collector/observation-sessions/" + session + "/audit-candidates"
	forwarded := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if !candidate(line) {
			continue
		}
		input := customercollector.ForwardedAuditInput{ClaimedClusterIdentity: cluster, Event: json.RawMessage(line)}
		body, err := json.Marshal(input)
		if err != nil {
			log.Fatal(err)
		}
		if err = forward(client, endpoint, token, body); err != nil {
			log.Fatal(err)
		}
		forwarded++
	}
	if err = scanner.Err(); err != nil {
		log.Fatal(err)
	}
	log.Printf("forwarded %d collector-authenticated audit candidates; API-server origin remains unverified", forwarded)
}

func candidate(line []byte) bool {
	events, err := kubeaudit.Parse(bytes.NewReader(line))
	if err != nil || len(events) != 1 {
		return false
	}
	e := events[0]
	return e.Stage == "ResponseComplete" && (e.Verb == "patch" || e.Verb == "update") && e.ObjectRef.APIGroup == "apps" && e.ObjectRef.APIVersion == "v1" && e.ObjectRef.Resource == "deployments" && e.Response.Code >= 200 && e.Response.Code < 300
}

func forward(client *http.Client, endpoint, token string, body []byte) error {
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err == nil {
			io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusOK {
				cancel()
				return nil
			}
			if response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
				cancel()
				return fmt.Errorf("audit submission rejected with HTTP %d", response.StatusCode)
			}
		}
		cancel()
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return fmt.Errorf("audit submission failed after three attempts")
}
