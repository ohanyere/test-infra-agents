package trajectory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// MaxPreviewBytes bounds inline previews so the main trajectory response
// never ships megabytes of logs.
const MaxPreviewBytes = 4096

// RedactionMarker replaces secret material.
const RedactionMarker = "[REDACTED]"

// sensitiveKeys are matched case-insensitively on exact key or suffix.
var sensitiveKeys = []string{
	"authorization", "cookie", "cookies", "api_key", "apikey", "token",
	"bearer", "password", "secret", "secrets", "kubeconfig", "private_key",
	"client_secret", "access_key", "refresh_token", "session_token",
	"credential", "credentials", "connection", "mcp_grant", "tool_access",
	"incident-secret", "provider_credential",
}

// RedactObject deep-copies value with sensitive keys replaced. Lists under a
// sensitive key are replaced wholesale; free strings keep operational context
// but bearer-looking literals are masked.
func RedactObject(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		lower := strings.ToLower(key)
		sensitive := false
		for _, marker := range sensitiveKeys {
			if lower == marker || strings.HasSuffix(lower, "_"+marker) || strings.HasSuffix(lower, "-"+marker) {
				sensitive = true
				break
			}
		}
		if sensitive {
			out[key] = RedactionMarker
			continue
		}
		out[key] = redactValue(item)
	}
	return out
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return RedactObject(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item)
		}
		return out
	case string:
		return redactString(typed)
	default:
		return value
	}
}

func redactString(text string) string {
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") {
		return RedactionMarker
	}
	return text
}

// SafeMetadata marshals an already-safe map after redaction. Unknown or nil
// input yields {}.
func SafeMetadata(value map[string]any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`{}`)
	}
	raw, err := json.Marshal(RedactObject(value))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// BoundedPreview returns a bounded text/json preview with truncation flag,
// original byte size, and sha256 content hash of the full input.
func BoundedPreview(full string) (preview string, truncated bool, byteSize int, hash string) {
	byteSize = len(full)
	sum := sha256.Sum256([]byte(full))
	hash = hex.EncodeToString(sum[:])
	if len(full) <= MaxPreviewBytes {
		return full, false, byteSize, hash
	}
	return full[:MaxPreviewBytes], true, byteSize, hash
}

// RedactRawMessage parses a JSON object payload, redacts it, and re-marshals.
// Non-object or invalid input yields {}.
func RedactRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return json.RawMessage(`{}`)
	}
	return SafeMetadata(object)
}
