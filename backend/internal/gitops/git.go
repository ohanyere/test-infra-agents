package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type GitReader struct {
	Root, Repository, Revision, ManifestPath string
	Target                                   WorkloadTarget
	Now                                      func() time.Time
}

type manifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name, Namespace string
		Labels          map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Replicas *int32 `yaml:"replicas"`
		Template struct {
			Metadata struct {
				Labels map[string]string `yaml:"labels"`
			} `yaml:"metadata"`
			Spec struct {
				Containers []struct {
					Name, Image string
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func (r GitReader) Read() (DesiredState, error) {
	if !revisionPattern.MatchString(r.Revision) {
		return DesiredState{}, errors.New("exact 40-character lowercase Git revision is required")
	}
	if r.Target.Kind != "Deployment" || r.Target.Name == "" || r.Target.Container == "" {
		return DesiredState{}, errors.New("exact Deployment and container target are required")
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return DesiredState{}, errors.New("invalid repository root")
	}
	head, err := readHead(root)
	if err != nil || head != r.Revision {
		return DesiredState{}, errors.New("repository HEAD does not match the pinned revision")
	}
	clean := filepath.Clean(r.ManifestPath)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return DesiredState{}, errors.New("manifest path must remain inside the repository")
	}
	path := filepath.Join(root, clean)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return DesiredState{}, errors.New("read pinned manifest")
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return DesiredState{}, errors.New("manifest symlink leaves repository")
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		return DesiredState{}, errors.New("read pinned manifest")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var value manifest
		err = decoder.Decode(&value)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return DesiredState{}, errors.New("malformed Kubernetes manifest")
		}
		if value.Kind != r.Target.Kind || value.Metadata.Name != r.Target.Name || (r.Target.Namespace != "" && value.Metadata.Namespace != "" && value.Metadata.Namespace != r.Target.Namespace) {
			continue
		}
		for _, container := range value.Spec.Template.Spec.Containers {
			if container.Name != r.Target.Container {
				continue
			}
			now := time.Now
			if r.Now != nil {
				now = r.Now
			}
			namespace := value.Metadata.Namespace
			if namespace == "" {
				namespace = r.Target.Namespace
			}
			return DesiredState{Repository: safeRepository(r.Repository), Revision: r.Revision, Path: filepath.ToSlash(clean), Resource: fmt.Sprintf("%s/%s/%s", r.Target.Kind, namespace, value.Metadata.Name), Container: container.Name, Image: container.Image, Replicas: value.Spec.Replicas, Labels: safeLabels(value.Spec.Template.Metadata.Labels), CapturedAt: now().UTC(), Reference: "git:" + r.Revision + ":" + filepath.ToSlash(clean)}, nil
		}
		return DesiredState{}, errors.New("target container is missing from pinned manifest")
	}
	return DesiredState{}, errors.New("target workload is missing from pinned manifest")
}

func safeRepository(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "repository"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func readHead(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(raw))
	if strings.HasPrefix(head, "ref: ") {
		ref := strings.TrimPrefix(head, "ref: ")
		if strings.Contains(ref, "..") || strings.HasPrefix(ref, "/") {
			return "", errors.New("unsafe Git ref")
		}
		raw, err = os.ReadFile(filepath.Join(root, ".git", filepath.FromSlash(ref)))
		if err != nil {
			return "", err
		}
		head = strings.TrimSpace(string(raw))
	}
	if !revisionPattern.MatchString(head) {
		return "", errors.New("unsupported Git HEAD")
	}
	return head, nil
}

func safeLabels(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		if key == "app" || key == "app.kubernetes.io/name" || key == "app.kubernetes.io/component" {
			out[key] = value
		}
	}
	return out
}
