# Incident security boundary

Realverax scenario authoring accepts bounded `.yaml`, `.yml`, `.json`, `.txt`,
`.log`, and `.md` incident inputs. Before any external AI provider is called,
the server parses supported structured files and applies the deterministic
`incident-redaction:v1` contract.

The sanitizer removes Kubernetes `Secret.data` and `Secret.stringData`, redacts
known sensitive structured keys recursively, and removes high-confidence
credentials from text, authorization headers, connection URLs, environment
assignments, and private-key blocks. Safe operational context such as resource
identity, topology, replica counts, latency, and request rate is retained.

Raw uploaded or pasted incident content is not persisted by the current
scenario-authoring pipeline. Providers receive only the normalized redacted
representation, and generated drafts are sanitized again before they can become
a ScenarioVersion.

Redaction is defense in depth, not a guarantee that every unknown secret format
can be recognized. Engineers should avoid uploading credentials or other
unnecessary sensitive material.
