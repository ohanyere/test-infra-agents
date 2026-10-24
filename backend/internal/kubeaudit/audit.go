package kubeaudit

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed audit-policy.yaml
var auditPolicy []byte

const (
	AuditAPIVersion = "audit.k8s.io/v1"
	kindClusterName = "evalplane-e2e"
	kindNodeName    = "evalplane-e2e-control-plane"
	auditLogDir     = "/var/log/kubernetes/audit"
	auditLogPath    = "/var/log/kubernetes/audit/audit.log"
	maxAuditLogSize = 32 << 20
	maxRecordSize   = 1 << 20
)

var ErrInvalidSource = errors.New("invalid Kubernetes audit source")
var ErrFailedOperation = errors.New("Kubernetes API operation failed")

type SourceIdentity struct {
	SourceType            string    `json:"source_type"`
	ClusterIdentity       string    `json:"cluster_identity"`
	ConfigurationIdentity string    `json:"configuration_identity"`
	AcquisitionLocation   string    `json:"acquisition_location"`
	AcquisitionMethod     string    `json:"acquisition_method"`
	VerificationState     string    `json:"verification_state"`
	ProvisioningIdentity  string    `json:"provisioning_identity"`
	AcquiredAt            time.Time `json:"acquired_at"`
	ProvenanceLimitations []string  `json:"provenance_limitations"`
}

type Event struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	AuditID    string    `json:"auditID"`
	Stage      string    `json:"stage"`
	Verb       string    `json:"verb"`
	RequestURI string    `json:"requestURI"`
	RequestAt  time.Time `json:"requestReceivedTimestamp"`
	StageAt    time.Time `json:"stageTimestamp"`
	User       User      `json:"user"`
	ObjectRef  ObjectRef `json:"objectRef"`
	Response   Response  `json:"responseStatus"`
}

type User struct {
	Username string   `json:"username"`
	UID      string   `json:"uid,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

type ObjectRef struct {
	APIGroup   string `json:"apiGroup"`
	APIVersion string `json:"apiVersion"`
	Resource   string `json:"resource"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
}

type Response struct {
	Code   int32  `json:"code"`
	Reason string `json:"reason"`
}

type AcquiredEvent struct {
	event  Event
	source SourceIdentity
	seal   [sha256.Size]byte
	sealed bool
}

func (e AcquiredEvent) RunnerBound() bool {
	if !e.sealed || e.source.VerificationState != "runner_bound_test_source" {
		return false
	}
	digest := acquiredEventDigest(e.event, e.source)
	return subtle.ConstantTimeCompare(e.seal[:], digest[:]) == 1
}

func (e AcquiredEvent) Snapshot() (Event, SourceIdentity, bool) {
	if !e.RunnerBound() {
		return Event{}, SourceIdentity{}, false
	}
	return cloneEvent(e.event), cloneSourceIdentity(e.source), true
}

func newAcquiredEvent(event Event, source SourceIdentity) AcquiredEvent {
	event = cloneEvent(event)
	source = cloneSourceIdentity(source)
	return AcquiredEvent{event: event, source: source, seal: acquiredEventDigest(event, source), sealed: true}
}

func acquiredEventDigest(event Event, source SourceIdentity) [sha256.Size]byte {
	encoded, _ := json.Marshal(struct {
		Event  Event          `json:"event"`
		Source SourceIdentity `json:"source"`
	}{event, source})
	return sha256.Sum256(encoded)
}

func cloneEvent(event Event) Event {
	event.User.Groups = append([]string(nil), event.User.Groups...)
	return event
}

func cloneSourceIdentity(source SourceIdentity) SourceIdentity {
	source.ProvenanceLimitations = append([]string(nil), source.ProvenanceLimitations...)
	return source
}

func SelectMutation(events []AcquiredEvent, namespace, name, verb, principal string) (AcquiredEvent, error) {
	var failedResponse int32
	for _, acquired := range events {
		if !acquired.RunnerBound() || acquired.source.ClusterIdentity != "kind:"+kindClusterName || acquired.source.ConfigurationIdentity != configurationIdentity() {
			continue
		}
		event := acquired.event
		if event.Stage != "ResponseComplete" || event.Verb != verb || event.ObjectRef.APIGroup != "apps" || event.ObjectRef.APIVersion != "v1" || event.ObjectRef.Resource != "deployments" || event.ObjectRef.Namespace != namespace || event.ObjectRef.Name != name {
			continue
		}
		if principal != "" && event.User.Username != principal {
			continue
		}
		if event.Response.Code < 200 || event.Response.Code >= 300 {
			failedResponse = event.Response.Code
			continue
		}
		return acquired, nil
	}
	if failedResponse != 0 {
		return AcquiredEvent{}, fmt.Errorf("%w: response code %d", ErrFailedOperation, failedResponse)
	}
	return AcquiredEvent{}, fmt.Errorf("%w: matching acquired audit event not found", ErrInvalidSource)
}

// SelectResponse returns the authenticated API-server response even when RBAC
// denied the operation. It is intentionally distinct from SelectMutation,
// which requires a successful mutation for its existing callers.
func SelectResponse(events []AcquiredEvent, namespace, name, verb, principal string) (AcquiredEvent, error) {
	for _, acquired := range events {
		if !acquired.RunnerBound() || acquired.source.ClusterIdentity != "kind:"+kindClusterName || acquired.source.ConfigurationIdentity != configurationIdentity() {
			continue
		}
		event := acquired.event
		if event.Stage == "ResponseComplete" && event.Verb == verb && event.ObjectRef.APIGroup == "apps" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "deployments" && event.ObjectRef.Namespace == namespace && event.ObjectRef.Name == name && event.User.Username == principal {
			return acquired, nil
		}
	}
	return AcquiredEvent{}, fmt.Errorf("%w: matching acquired audit response not found", ErrInvalidSource)
}

func Parse(reader io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxRecordSize)
	var events []Event
	seen := make(map[string]struct{})
	for line := 1; scanner.Scan(); line++ {
		data := scanner.Bytes()
		if len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, fmt.Errorf("audit record line %d: %w", line, err)
		}
		if event.APIVersion != AuditAPIVersion || event.Kind != "Event" {
			return nil, fmt.Errorf("audit record line %d: %w: unsupported event type", line, ErrInvalidSource)
		}
		if strings.TrimSpace(event.AuditID) == "" || strings.TrimSpace(event.Stage) == "" {
			return nil, fmt.Errorf("audit record line %d: %w: missing event identity or stage", line, ErrInvalidSource)
		}
		if event.Stage != "RequestReceived" && event.Stage != "ResponseStarted" && event.Stage != "ResponseComplete" && event.Stage != "Panic" {
			return nil, fmt.Errorf("audit record line %d: %w: unsupported audit stage", line, ErrInvalidSource)
		}
		key := event.AuditID + "\x00" + event.Stage
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("audit record line %d: %w: duplicate audit stage", line, ErrInvalidSource)
		}
		seen[key] = struct{}{}
		if event.Stage != "ResponseComplete" {
			continue
		}
		if event.Verb == "" || event.ObjectRef.APIVersion == "" || event.ObjectRef.Resource == "" || event.ObjectRef.Name == "" || event.RequestAt.IsZero() || event.StageAt.IsZero() || event.Response.Code < 100 || event.Response.Code > 599 {
			return nil, fmt.Errorf("audit record line %d: %w: incomplete response event", line, ErrInvalidSource)
		}
		if event.RequestAt.After(event.StageAt) {
			return nil, fmt.Errorf("audit record line %d: %w: invalid event timestamps", line, ErrInvalidSource)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	return events, nil
}

func AcquireHostedKind(ctx context.Context) ([]AcquiredEvent, SourceIdentity, error) {
	runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
	if os.Getenv("GITHUB_ACTIONS") != "true" || runID == "" {
		return nil, SourceIdentity{}, fmt.Errorf("%w: acquisition is restricted to a GitHub Actions run", ErrInvalidSource)
	}
	if err := verifyKindBinding(ctx); err != nil {
		return nil, SourceIdentity{}, err
	}
	directory, err := os.MkdirTemp("", "realverax-kind-audit-")
	if err != nil {
		return nil, SourceIdentity{}, fmt.Errorf("create private audit acquisition directory: %w", err)
	}
	defer os.RemoveAll(directory)
	if err = os.Chmod(directory, 0700); err != nil {
		return nil, SourceIdentity{}, fmt.Errorf("protect audit acquisition directory: %w", err)
	}
	localPath := filepath.Join(directory, "audit.log")
	if err = run(ctx, "docker", "cp", kindNodeName+":"+auditLogPath, localPath); err != nil {
		return nil, SourceIdentity{}, fmt.Errorf("acquire audit log from provisioned Kind control plane: %w", err)
	}
	info, err := os.Stat(localPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxAuditLogSize {
		return nil, SourceIdentity{}, fmt.Errorf("%w: audit log missing or outside size limit", ErrInvalidSource)
	}
	file, err := os.Open(localPath)
	if err != nil {
		return nil, SourceIdentity{}, fmt.Errorf("open acquired audit log: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, maxAuditLogSize+1))
	events, parseErr := Parse(bytes.NewReader(contents))
	closeErr := file.Close()
	if readErr != nil {
		return nil, SourceIdentity{}, fmt.Errorf("read acquired audit log: %w", readErr)
	}
	if len(contents) > maxAuditLogSize {
		return nil, SourceIdentity{}, fmt.Errorf("%w: audit log exceeds size limit", ErrInvalidSource)
	}
	if parseErr != nil {
		return nil, SourceIdentity{}, parseErr
	}
	if closeErr != nil {
		return nil, SourceIdentity{}, closeErr
	}
	identity := SourceIdentity{
		SourceType: "kubernetes-apiserver-audit", ClusterIdentity: "kind:" + kindClusterName,
		ConfigurationIdentity: configurationIdentity(), AcquisitionLocation: kindNodeName + ":" + auditLogPath,
		AcquisitionMethod: "runner-docker-copy-from-kind-control-plane", VerificationState: "runner_bound_test_source",
		ProvisioningIdentity: "github-actions:" + runID + ":kind-create-cluster:" + kindClusterName, AcquiredAt: time.Now().UTC(),
		ProvenanceLimitations: []string{"runner controls the disposable Kind cluster and acquisition host", "not a production customer-cluster authenticator", "file location and checksum are not cryptographic source authentication"},
	}
	acquired := make([]AcquiredEvent, 0, len(events))
	for _, event := range events {
		acquired = append(acquired, newAcquiredEvent(event, identity))
	}
	return acquired, identity, nil
}

func WaitForMutation(ctx context.Context, namespace, name, verb, principal string) (AcquiredEvent, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		events, _, err := AcquireHostedKind(ctx)
		if err == nil {
			if selected, selectErr := SelectMutation(events, namespace, name, verb, principal); selectErr == nil {
				return selected, nil
			} else {
				if errors.Is(selectErr, ErrFailedOperation) {
					return AcquiredEvent{}, selectErr
				}
				lastErr = selectErr
			}
		} else {
			if errors.Is(err, ErrInvalidSource) {
				return AcquiredEvent{}, err
			}
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return AcquiredEvent{}, fmt.Errorf("wait for Kubernetes audit event: %w: %v", ctx.Err(), lastErr)
			}
			return AcquiredEvent{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// WaitForResponse observes both successful and denied direct API requests.
// A missing response is evidence absence, never a successful operation.
func WaitForResponse(ctx context.Context, namespace, name, verb, principal string) (AcquiredEvent, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		events, _, err := AcquireHostedKind(ctx)
		if err == nil {
			if selected, selectErr := SelectResponse(events, namespace, name, verb, principal); selectErr == nil {
				return selected, nil
			} else {
				lastErr = selectErr
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return AcquiredEvent{}, fmt.Errorf("wait for Kubernetes audit response: %w: %v", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func verifyKindBinding(ctx context.Context) error {
	clusters, err := output(ctx, "kind", "get", "clusters")
	if err != nil || !containsLine(clusters, kindClusterName) {
		return fmt.Errorf("%w: expected Kind cluster is not provisioned", ErrInvalidSource)
	}
	currentContext, err := output(ctx, "kubectl", "config", "current-context")
	if err != nil || strings.TrimSpace(currentContext) != "kind-"+kindClusterName {
		return fmt.Errorf("%w: Kubernetes context does not match expected cluster", ErrInvalidSource)
	}
	nodes, err := output(ctx, "kind", "get", "nodes", "--name", kindClusterName)
	if err != nil || !containsLine(nodes, kindNodeName) {
		return fmt.Errorf("%w: expected Kind control-plane node is not provisioned", ErrInvalidSource)
	}
	label, err := output(ctx, "docker", "inspect", "--format", `{{ index .Config.Labels "io.x-k8s.kind.cluster" }}`, kindNodeName)
	if err != nil || strings.TrimSpace(label) != kindClusterName {
		return fmt.Errorf("%w: control-plane container cluster label mismatch", ErrInvalidSource)
	}
	policy, err := output(ctx, "docker", "exec", kindNodeName, "cat", "/etc/kubernetes/policies/audit-policy.yaml")
	if err != nil || sha256.Sum256([]byte(policy)) != sha256.Sum256(auditPolicy) {
		return fmt.Errorf("%w: mounted audit policy does not match the expected configuration", ErrInvalidSource)
	}
	manifest, err := output(ctx, "docker", "exec", kindNodeName, "cat", "/etc/kubernetes/manifests/kube-apiserver.yaml")
	if err != nil {
		return fmt.Errorf("%w: unable to read kube-apiserver static pod manifest", ErrInvalidSource)
	}
	if err := validateAuditArguments([]byte(manifest)); err != nil {
		return fmt.Errorf("%w: static manifest: %w", ErrInvalidSource, err)
	}
	containers, err := output(ctx, "docker", "exec", kindNodeName, "crictl", "ps", "--name", "kube-apiserver", "-q")
	containerIDs := strings.Fields(containers)
	if err != nil || len(containerIDs) != 1 {
		return fmt.Errorf("%w: expected one running kube-apiserver container; found %d", ErrInvalidSource, len(containerIDs))
	}
	inspection, err := output(ctx, "docker", "exec", kindNodeName, "crictl", "inspect", containerIDs[0])
	if err != nil {
		return fmt.Errorf("%w: unable to inspect running kube-apiserver container", ErrInvalidSource)
	}
	arguments, err := runningProcessArguments([]byte(inspection))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	if err := validateAuditArgumentValues(arguments); err != nil {
		return fmt.Errorf("%w: running process: %w", ErrInvalidSource, err)
	}
	if err := validateRuntimeAuditMounts([]byte(inspection)); err != nil {
		return fmt.Errorf("%w: running process mounts: %w", ErrInvalidSource, err)
	}
	return nil
}

func validateAuditArguments(manifest []byte) error {
	var pod struct {
		Spec struct {
			Volumes []struct {
				Name     string `yaml:"name"`
				HostPath struct {
					Path string `yaml:"path"`
					Type string `yaml:"type"`
				} `yaml:"hostPath"`
			} `yaml:"volumes"`
			Containers []struct {
				Name         string   `yaml:"name"`
				Command      []string `yaml:"command"`
				Args         []string `yaml:"args"`
				VolumeMounts []struct {
					Name      string `yaml:"name"`
					MountPath string `yaml:"mountPath"`
					ReadOnly  bool   `yaml:"readOnly"`
				} `yaml:"volumeMounts"`
			} `yaml:"containers"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(manifest, &pod); err != nil {
		return errors.New("kube-apiserver manifest is not valid YAML")
	}
	var arguments []string
	var volumeMounts []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
		ReadOnly  bool   `yaml:"readOnly"`
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == "kube-apiserver" {
			arguments = append(append(arguments, container.Command...), container.Args...)
			volumeMounts = container.VolumeMounts
			break
		}
	}
	if arguments == nil {
		return errors.New("kube-apiserver container arguments are missing")
	}
	if err := validateAuditArgumentValues(arguments); err != nil {
		return err
	}
	return validateAuditVolumeMounts(pod.Spec.Volumes, volumeMounts)
}

func validateAuditArgumentValues(arguments []string) error {
	expected := map[string]string{
		"audit-policy-file":   "/etc/kubernetes/policies/audit-policy.yaml",
		"audit-log-path":      auditLogPath,
		"audit-log-mode":      "blocking",
		"audit-log-maxage":    "1",
		"audit-log-maxbackup": "2",
		"audit-log-maxsize":   "20",
	}
	values := make(map[string][]string, len(expected))
	for name := range expected {
		values[name] = nil
	}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if !strings.HasPrefix(argument, "--") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(argument, "--"), "=")
		if _, required := values[name]; !required {
			continue
		}
		if !hasValue && index+1 < len(arguments) && !strings.HasPrefix(arguments[index+1], "--") {
			index++
			value = arguments[index]
			hasValue = true
		}
		if !hasValue {
			value = ""
		}
		values[name] = append(values[name], value)
	}
	names := []string{"audit-policy-file", "audit-log-path", "audit-log-mode", "audit-log-maxage", "audit-log-maxbackup", "audit-log-maxsize"}
	sort.Strings(names)
	var mismatches []string
	for _, name := range names {
		expectedValue := expected[name]
		observed := values[name]
		switch {
		case len(observed) == 0:
			mismatches = append(mismatches, fmt.Sprintf("%s missing (expected %s)", name, expectedValue))
		case len(observed) != 1:
			mismatches = append(mismatches, fmt.Sprintf("%s duplicated (expected one argument)", name))
		case observed[0] != expectedValue:
			mismatches = append(mismatches, fmt.Sprintf("%s mismatched (expected %s)", name, expectedValue))
		}
	}
	if len(mismatches) != 0 {
		return fmt.Errorf("API-server audit argument mismatch: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

func validateAuditVolumeMounts(volumes []struct {
	Name     string `yaml:"name"`
	HostPath struct {
		Path string `yaml:"path"`
		Type string `yaml:"type"`
	} `yaml:"hostPath"`
}, mounts []struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly"`
}) error {
	type expectedMount struct {
		name     string
		path     string
		pathType string
		readOnly bool
	}
	expected := []expectedMount{
		{name: "audit-policies", path: "/etc/kubernetes/policies/audit-policy.yaml", pathType: "File", readOnly: true},
		{name: "audit-logs", path: auditLogDir, pathType: "DirectoryOrCreate", readOnly: false},
	}
	for _, want := range expected {
		volumeCount := 0
		for _, volume := range volumes {
			if volume.Name == want.name && volume.HostPath.Path == want.path && volume.HostPath.Type == want.pathType {
				volumeCount++
			}
		}
		mountCount := 0
		for _, mount := range mounts {
			if mount.Name == want.name && mount.MountPath == want.path && mount.ReadOnly == want.readOnly {
				mountCount++
			}
		}
		if volumeCount != 1 || mountCount != 1 {
			return fmt.Errorf("%s audit volume mount missing or mismatched", want.name)
		}
	}
	return nil
}

func runningProcessArguments(inspection []byte) ([]string, error) {
	var container struct {
		Info struct {
			RuntimeSpec struct {
				Process struct {
					Args []string `json:"args"`
				} `json:"process"`
			} `json:"runtimeSpec"`
		} `json:"info"`
	}
	if err := json.Unmarshal(inspection, &container); err != nil {
		return nil, errors.New("running kube-apiserver process metadata is invalid")
	}
	if len(container.Info.RuntimeSpec.Process.Args) == 0 {
		return nil, errors.New("running kube-apiserver process arguments are unavailable")
	}
	return container.Info.RuntimeSpec.Process.Args, nil
}

func validateRuntimeAuditMounts(inspection []byte) error {
	var container struct {
		Info struct {
			RuntimeSpec struct {
				Mounts []struct {
					Destination string   `json:"destination"`
					Options     []string `json:"options"`
				} `json:"mounts"`
			} `json:"runtimeSpec"`
		} `json:"info"`
	}
	if err := json.Unmarshal(inspection, &container); err != nil {
		return errors.New("running kube-apiserver mount metadata is invalid")
	}
	expected := []struct {
		path     string
		readOnly bool
		label    string
	}{
		{path: "/etc/kubernetes/policies/audit-policy.yaml", readOnly: true, label: "audit policy"},
		{path: auditLogDir, readOnly: false, label: "audit log directory"},
	}
	for _, want := range expected {
		count := 0
		readOnly := false
		for _, mount := range container.Info.RuntimeSpec.Mounts {
			if mount.Destination != want.path {
				continue
			}
			count++
			for _, option := range mount.Options {
				if option == "ro" {
					readOnly = true
				}
			}
		}
		if count != 1 || readOnly != want.readOnly {
			return fmt.Errorf("%s runtime mount missing or has incorrect access mode", want.label)
		}
	}
	return nil
}

func configurationIdentity() string {
	digest := sha256.Sum256(auditPolicy)
	return "kind-audit-policy:sha256:" + hex.EncodeToString(digest[:])
}

func run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %s: %w", name, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	result, err := command.Output()
	return string(result), err
}

func containsLine(value, expected string) bool {
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}
