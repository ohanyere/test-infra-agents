package scenarioauthoring

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

const (
	RedactionMarker   = "[REDACTED]"
	RedactionContract = "incident-redaction:v1"
)

var (
	authorizationPattern = regexp.MustCompile(`(?i)\b(authorization|proxy[-_ ]?authorization)(\s*:\s*)(bearer|basic)(\s+)([^\s,;]+)`)
	credentialPattern    = regexp.MustCompile(`(?i)["']?(password|passwd|pwd|secret|client[_-]?secret|token|access[_-]?token|refresh[_-]?token|id[_-]?token|api[_-]?key|apikey|private[_-]?key|cookie|set[_-]?cookie|database[_-]?password|db[_-]?password|aws[_-]?secret[_-]?access[_-]?key)["']?(\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
	userinfoPattern      = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9+.-]*://)([^/@\s:]+):([^/@\s]+)@`)
	privateKeyPattern    = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	githubTokenPattern   = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)
	awsAccessKeyPattern  = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
)

var sensitiveKeys = map[string]struct{}{
	"password": {}, "passwd": {}, "pwd": {}, "secret": {}, "clientsecret": {},
	"token": {}, "accesstoken": {}, "refreshtoken": {}, "idtoken": {},
	"apikey": {}, "apitoken": {}, "privatekey": {}, "authorization": {}, "proxyauthorization": {},
	"cookie": {}, "setcookie": {}, "databasepassword": {}, "dbpassword": {},
	"awssecretaccesskey": {}, "awsaccesskeyid": {},
}

// Sanitizer is the single deterministic security boundary shared by incident
// uploads, pasted incident text, provider input, and provider-produced drafts.
type Sanitizer struct{}

type redactionContext struct {
	literals map[string]struct{}
}

func NewSanitizer() Sanitizer { return Sanitizer{} }

func newRedactionContext() *redactionContext {
	return &redactionContext{literals: map[string]struct{}{}}
}

func (c *redactionContext) add(value string) {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" || value == RedactionMarker {
		return
	}
	c.literals[value] = struct{}{}
}

func (c *redactionContext) addValue(value any) {
	switch typed := value.(type) {
	case string:
		c.add(typed)
	case map[string]any:
		for _, item := range typed {
			c.addValue(item)
		}
	case []any:
		for _, item := range typed {
			c.addValue(item)
		}
	}
}

func RedactSecrets(text string) string {
	return NewSanitizer().redactText(text, newRedactionContext())
}

func (Sanitizer) redactText(text string, context *redactionContext) string {
	text = privateKeyPattern.ReplaceAllStringFunc(text, func(match string) string {
		context.add(match)
		for _, line := range strings.Split(match, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "-----BEGIN ") && !strings.HasPrefix(line, "-----END ") {
				context.add(line)
			}
		}
		return RedactionMarker
	})
	text = authorizationPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := authorizationPattern.FindStringSubmatch(match)
		context.add(parts[5])
		return parts[1] + parts[2] + parts[3] + parts[4] + RedactionMarker
	})
	text = userinfoPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := userinfoPattern.FindStringSubmatch(match)
		context.add(parts[3])
		return parts[1] + parts[2] + ":" + RedactionMarker + "@"
	})
	text = credentialPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := credentialPattern.FindStringSubmatch(match)
		context.add(parts[3])
		return parts[1] + parts[2] + RedactionMarker
	})
	for _, pattern := range []*regexp.Regexp{githubTokenPattern, awsAccessKeyPattern} {
		text = pattern.ReplaceAllStringFunc(text, func(match string) string {
			context.add(match)
			return RedactionMarker
		})
	}
	return text
}

func (s Sanitizer) redactKnown(text string, context *redactionContext) string {
	values := make([]string, 0, len(context.literals))
	for value := range context.literals {
		// Short ordinary words are too destructive as context-free matches. They
		// remain protected whenever they occur under a sensitive key.
		if len(value) >= 8 {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		text = strings.ReplaceAll(text, value, RedactionMarker)
	}
	return s.redactText(text, context)
}

func normalizedSecretKey(key string) string {
	var out strings.Builder
	for _, character := range strings.ToLower(key) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			out.WriteRune(character)
		}
	}
	return out.String()
}

func isSensitiveKey(key string) bool {
	_, ok := sensitiveKeys[normalizedSecretKey(key)]
	return ok
}

func (s Sanitizer) sanitizeValue(value any, context *redactionContext) any {
	switch typed := value.(type) {
	case map[string]any:
		kind, _ := typed["kind"].(string)
		isKubernetesSecret := strings.EqualFold(kind, "Secret")
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isKubernetesSecret && (strings.EqualFold(key, "data") || strings.EqualFold(key, "stringData")) {
				context.addValue(item)
				out[key] = RedactionMarker
				continue
			}
			if isSensitiveKey(key) {
				context.addValue(item)
				out[key] = RedactionMarker
				continue
			}
			out[key] = s.sanitizeValue(item, context)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = s.sanitizeValue(item, context)
		}
		return out
	case string:
		return s.redactKnown(typed, context)
	default:
		return typed
	}
}

func collectSensitiveValues(value any, context *redactionContext) {
	switch typed := value.(type) {
	case map[string]any:
		kind, _ := typed["kind"].(string)
		for key, item := range typed {
			if isSensitiveKey(key) || strings.EqualFold(kind, "Secret") && (strings.EqualFold(key, "data") || strings.EqualFold(key, "stringData")) {
				context.addValue(item)
			}
			collectSensitiveValues(item, context)
		}
	case []any:
		for _, item := range typed {
			collectSensitiveValues(item, context)
		}
	}
}

func (s Sanitizer) SanitizeDraft(draft ScenarioDraft, context *redactionContext) ScenarioDraft {
	if context == nil {
		context = newRedactionContext()
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return draft
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return draft
	}
	collectSensitiveValues(value, context)
	safe, err := json.Marshal(s.sanitizeValue(value, context))
	if err != nil || json.Unmarshal(safe, &draft) != nil {
		return draft
	}
	if string(draft.SourceMetadata) == "null" {
		draft.SourceMetadata = nil
	}
	return draft
}

func (s Sanitizer) sanitizeProviderIncident(input IncidentInput) IncidentInput {
	context := newRedactionContext()
	input.Description = s.redactText(input.Description, context)
	input.Timeline = s.redactText(input.Timeline, context)
	input.SanitizedEvidence = s.redactText(input.SanitizedEvidence, context)
	input.EvidenceSummary = append([]string(nil), input.EvidenceSummary...)
	for index := range input.EvidenceSummary {
		input.EvidenceSummary[index] = s.redactText(input.EvidenceSummary[index], context)
	}
	input.Facts = append([]string(nil), input.Facts...)
	for index := range input.Facts {
		input.Facts[index] = s.redactText(input.Facts[index], context)
	}
	input.Files = nil
	return input
}
