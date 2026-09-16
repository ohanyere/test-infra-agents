package scenarioauthoring

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const (
	MaxFiles             = 10
	MaxFileSize          = 5 << 20
	MaxTotalSize         = 20 << 20
	MaxSanitizedEvidence = 200000
)

func NormalizeIncident(input IncidentInput) (IncidentInput, error) {
	output, _, err := NewSanitizer().SanitizeIncident(input)
	return output, err
}

func (s Sanitizer) SanitizeIncident(input IncidentInput) (IncidentInput, *redactionContext, error) {
	context := newRedactionContext()
	output := IncidentInput{}
	if len(input.Files) > MaxFiles {
		return output, context, errors.New("at most 10 incident files are allowed")
	}
	if len(input.Description)+len(input.Timeline) > MaxSanitizedEvidence {
		return output, context, errors.New("incident text exceeds the analysis limit")
	}
	total := 0
	parts := []string{}
	output.Description = normalizeText(s.redactText(input.Description, context))
	output.Timeline = normalizeText(s.redactText(input.Timeline, context))
	if output.Description != "" {
		parts = append(parts, "DESCRIPTION:\n"+output.Description)
	}
	if output.Timeline != "" {
		parts = append(parts, "TIMELINE:\n"+output.Timeline)
	}
	for _, file := range input.Files {
		if len(file.Content) > MaxFileSize {
			return IncidentInput{}, context, errors.New("incident file exceeds 5 MiB")
		}
		total += len(file.Content)
		if total > MaxTotalSize {
			return IncidentInput{}, context, errors.New("incident files exceed 20 MiB total")
		}
		ext := strings.ToLower(filepath.Ext(file.Name))
		safeName := filepath.Base(s.redactText(filepath.Base(file.Name), context))
		switch ext {
		case ".yaml", ".yml", ".json":
			summary, facts, structured, e := extractStructured(s, context, file.Content, ext)
			if e != nil {
				return IncidentInput{}, context, errors.New("invalid structured incident file")
			}
			output.EvidenceSummary = append(output.EvidenceSummary, safeName+": "+summary)
			output.Facts = append(output.Facts, facts...)
			parts = append(parts, "STRUCTURED EVIDENCE:\n"+structured+"\nSTRUCTURED FACTS:\n"+strings.Join(facts, "\n"))
		case ".txt", ".log", ".md":
			text := normalizeText(s.redactText(string(file.Content), context))
			output.EvidenceSummary = append(output.EvidenceSummary, safeName+": sanitized text evidence")
			parts = append(parts, "TEXT EVIDENCE:\n"+text)
		default:
			return IncidentInput{}, context, errors.New("unsupported incident file type")
		}
	}
	output.SanitizedEvidence = strings.Join(parts, "\n\n")
	if len(output.SanitizedEvidence) > MaxSanitizedEvidence {
		output.SanitizedEvidence = output.SanitizedEvidence[:MaxSanitizedEvidence]
		output.EvidenceSummary = append(output.EvidenceSummary, "sanitized evidence was truncated to the analysis limit")
	}
	return output, context, nil
}
func normalizeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(value, "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
func extractStructured(s Sanitizer, context *redactionContext, content []byte, ext string) (string, []string, string, error) {
	var raw any
	if ext == ".json" {
		if json.Unmarshal(content, &raw) != nil {
			return "", nil, "", errors.New("invalid JSON")
		}
		raw = stringMap(raw)
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(content))
		documents := []any{}
		for {
			var document any
			if err := decoder.Decode(&document); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return "", nil, "", errors.New("invalid YAML")
			}
			if document != nil {
				documents = append(documents, stringMap(document))
			}
		}
		if len(documents) == 1 {
			raw = documents[0]
		} else {
			raw = documents
		}
	}
	collectSensitiveValues(raw, context)
	raw = s.sanitizeValue(raw, context)
	safeStructured, err := json.Marshal(raw)
	if err != nil {
		return "", nil, "", errors.New("structured incident could not be normalized")
	}
	items := []any{raw}
	if documents, ok := raw.([]any); ok {
		items = documents
	}
	if m, ok := raw.(map[string]any); ok {
		if list, ok := m["items"].([]any); ok {
			items = list
		}
	}
	facts := []string{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		metadata, _ := m["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		namespace, _ := metadata["namespace"].(string)
		if kind == "Secret" {
			facts = append(facts, fmt.Sprintf("kind=Secret name=%s namespace=%s contents=[REMOVED]", name, namespace))
			continue
		}
		facts = append(facts, fmt.Sprintf("kind=%s name=%s namespace=%s", kind, name, namespace))
		if spec, ok := m["spec"].(map[string]any); ok {
			if replicas, ok := spec["replicas"]; ok {
				facts = append(facts, fmt.Sprintf("%s/%s replicas=%v", kind, name, replicas))
			}
			extractContainers(spec, kind, name, &facts)
		}
	}
	sort.Strings(facts)
	if len(facts) == 0 {
		facts = []string{"Structured evidence parsed; no recognized Kubernetes object facts."}
	}
	return fmt.Sprintf("%d safe facts extracted", len(facts)), facts, string(safeStructured), nil
}
func extractContainers(spec map[string]any, kind, name string, facts *[]string) {
	template, _ := spec["template"].(map[string]any)
	podSpec, _ := template["spec"].(map[string]any)
	containers, _ := podSpec["containers"].([]any)
	for _, item := range containers {
		container, _ := item.(map[string]any)
		image, _ := container["image"].(string)
		containerName, _ := container["name"].(string)
		if image != "" {
			*facts = append(*facts, fmt.Sprintf("%s/%s container=%s image=%s", kind, name, containerName, image))
		}
		if ports, ok := container["ports"].([]any); ok {
			for _, p := range ports {
				if pm, ok := p.(map[string]any); ok {
					*facts = append(*facts, fmt.Sprintf("%s/%s container=%s port=%v", kind, name, containerName, pm["containerPort"]))
				}
			}
		}
		if resources, ok := container["resources"].(map[string]any); ok {
			safe, _ := json.Marshal(resources)
			*facts = append(*facts, fmt.Sprintf("%s/%s container=%s resources=%s", kind, name, containerName, safe))
		}
	}
}
func stringMap(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			v[k] = stringMap(x)
		}
		return v
	case map[any]any:
		m := map[string]any{}
		for k, x := range v {
			m[fmt.Sprint(k)] = stringMap(x)
		}
		return m
	case []any:
		for i := range v {
			v[i] = stringMap(v[i])
		}
		return v
	default:
		return v
	}
}
