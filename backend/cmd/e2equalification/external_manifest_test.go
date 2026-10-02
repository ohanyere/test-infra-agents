package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var credentialEnvPattern = regexp.MustCompile(`(?i)token|password|secret|credential|kubeconfig|private_key|api_key|bearer`)

// TestExternalAgentManifestsIsolateCredentials parses the external-agent
// Kubernetes manifests and proves the agent runtime receives no prohibited
// credentials: no credential-shaped env, no secret/config refs, no secret or
// host volumes, no privileged service account.
func TestExternalAgentManifestsIsolateCredentials(t *testing.T) {
	for _, file := range []string{"external-agent.yaml", "external-agent-hostile.yaml"} {
		seenDeployment, seenService := false, false
		for _, document := range splitManifestDocuments(t, file) {
			kind, _ := document["kind"].(string)
			switch kind {
			case "Deployment":
				seenDeployment = true
				assertAgentPodIsolated(t, file, deploymentPodSpec(t, file, document))
			case "Service":
				seenService = true
				assertAgentServiceShape(t, file, document)
			default:
				t.Fatalf("%s has unexpected kind %q", file, kind)
			}
		}
		if !seenDeployment || !seenService {
			t.Fatalf("%s must define a Deployment and a Service", file)
		}
	}
}

// TestExternalQualificationManifestsPinInClusterAgentURL ensures the
// qualification pods reach the external agent over plain in-cluster HTTP with
// no credential material in the agent URL. (DATABASE_URL and E2E_AUTH_PASSWORD
// belong to the harness, which owns database access by design.)
func TestExternalQualificationManifestsPinInClusterAgentURL(t *testing.T) {
	expect := map[string]string{
		"qualification-external.yaml": "http://external-agent.evalplane-e2e-harness.svc:8081",
		"qualification-hostile.yaml":  "http://external-agent-hostile.evalplane-e2e-harness.svc:8081",
	}
	for file, url := range expect {
		foundPod, foundURL := false, false
		for _, document := range splitManifestDocuments(t, file) {
			if document["kind"] != "Pod" {
				t.Fatalf("%s has unexpected kind %q", file, document["kind"])
			}
			foundPod = true
			podSpec := podManifestSpec(t, file, document)
			containers := podSpecContainers(t, file, podSpec)
			if len(containers) != 1 {
				t.Fatalf("%s must run exactly the harness container", file)
			}
			for _, env := range containerEnv(containers[0]) {
				name, _ := env["name"].(string)
				value, _ := env["value"].(string)
				if credentialEnvPattern.MatchString(name) && name != "DATABASE_URL" && name != "E2E_AUTH_PASSWORD" {
					t.Fatalf("%s qualification env %q looks like credential material", file, name)
				}
				if name == "EXTERNAL_AGENT_URL" {
					foundURL = true
					if value != url {
						t.Fatalf("%s agent URL=%q", file, value)
					}
				}
			}
		}
		if !foundPod || !foundURL {
			t.Fatalf("%s must define the qualification Pod with EXTERNAL_AGENT_URL", file)
		}
	}
}

func splitManifestDocuments(t *testing.T, file string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "e2e", file))
	if err != nil {
		t.Fatal(err)
	}
	var documents []map[string]any
	for _, part := range strings.Split(string(raw), "\n---") {
		lines := strings.Split(part, "\n")
		kept := lines[:0]
		for _, line := range lines {
			if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			kept = append(kept, line)
		}
		part = strings.TrimSpace(strings.Join(kept, "\n"))
		if part == "" {
			continue
		}
		var document map[string]any
		if err := yaml.Unmarshal([]byte(part), &document); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if len(document) > 0 {
			documents = append(documents, document)
		}
	}
	return documents
}

func deploymentPodSpec(t *testing.T, file string, document map[string]any) map[string]any {
	t.Helper()
	spec, _ := document["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	podSpec, _ := template["spec"].(map[string]any)
	if podSpec == nil {
		t.Fatalf("%s deployment has no pod spec", file)
	}
	return podSpec
}

func podManifestSpec(t *testing.T, file string, document map[string]any) map[string]any {
	t.Helper()
	podSpec, _ := document["spec"].(map[string]any)
	if podSpec == nil {
		t.Fatalf("%s pod has no spec", file)
	}
	return podSpec
}

func podSpecContainers(t *testing.T, file string, podSpec map[string]any) []map[string]any {
	t.Helper()
	raw, _ := podSpec["containers"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok || len(item) == 0 {
			t.Fatalf("%s has an empty container", file)
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no containers", file)
	}
	return out
}

func podSpecVolumes(podSpec map[string]any) []map[string]any {
	raw, _ := podSpec["volumes"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if volume, ok := entry.(map[string]any); ok {
			out = append(out, volume)
		}
	}
	return out
}

func containerEnv(container map[string]any) []map[string]any {
	raw, _ := container["env"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if item, ok := entry.(map[string]any); ok {
			out = append(out, item)
		}
	}
	return out
}

func assertAgentPodIsolated(t *testing.T, file string, podSpec map[string]any) {
	t.Helper()
	if name, _ := podSpec["serviceAccountName"].(string); name != "" {
		t.Fatalf("%s agent pod must use the default service account, got %q", file, name)
	}
	for _, volume := range podSpecVolumes(podSpec) {
		for key := range volume {
			switch key {
			case "name", "emptyDir", "configMap":
				// emptyDir/configMap carry no credentials by themselves, but the
				// agent manifests must not need volumes at all.
				t.Fatalf("%s agent pod mounts volume %q", file, key)
			default:
				t.Fatalf("%s agent pod mounts prohibited volume %q", file, key)
			}
		}
	}
	containers := podSpecContainers(t, file, podSpec)
	if len(containers) != 1 {
		t.Fatalf("%s must run exactly the agent container", file)
	}
	if _, ok := containers[0]["envFrom"]; ok {
		t.Fatalf("%s agent container uses envFrom", file)
	}
	for _, env := range containerEnv(containers[0]) {
		name, _ := env["name"].(string)
		if credentialEnvPattern.MatchString(name) {
			t.Fatalf("%s container env %q looks like credential material", file, name)
		}
		if _, ok := env["valueFrom"]; ok {
			t.Fatalf("%s container env %q references cluster state", file, name)
		}
	}
}

func assertAgentServiceShape(t *testing.T, file string, document map[string]any) {
	t.Helper()
	spec, _ := document["spec"].(map[string]any)
	ports, _ := spec["ports"].([]any)
	if len(ports) != 1 {
		t.Fatalf("%s service must expose exactly the protocol port", file)
	}
	port, _ := ports[0].(map[string]any)
	if port["port"] != 8081 {
		t.Fatalf("%s service port=%v", file, port["port"])
	}
}
