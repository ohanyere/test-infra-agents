package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureRepo(t *testing.T, manifest string) (string, string) {
	t.Helper()
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(revision+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "checkout.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	return root, revision
}

func TestGitReaderPinnedDeployment(t *testing.T) {
	root, revision := fixtureRepo(t, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  replicas: 3
  template:
    metadata: {labels: {app: checkout, secret-label: hidden}}
    spec:
      containers: [{name: checkout, image: checkout:v18, env: [{name: TOKEN, value: secret}]}]
`)
	got, err := (GitReader{Root: root, Repository: "https://token:secret@example.invalid/repo.git?credential=bad", Revision: revision, ManifestPath: "checkout.yaml", Target: WorkloadTarget{Kind: "Deployment", Namespace: "run-ns", Name: "checkout", Container: "checkout"}, Now: func() time.Time { return time.Unix(1, 0) }}).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "checkout:v18" || got.Resource != "Deployment/run-ns/checkout" || got.Labels["secret-label"] != "" {
		t.Fatalf("unsafe/wrong desired state: %+v", got)
	}
	if raw := got.Reference + got.Repository + strings.Join([]string{got.Image, got.Resource}, " "); strings.Contains(raw, "secret") || strings.Contains(raw, "token") || strings.Contains(raw, "credential") {
		t.Fatalf("secret leaked: %s", raw)
	}
}

func TestGitReaderRejectsWrongPinMalformedAndMissingTarget(t *testing.T) {
	root, revision := fixtureRepo(t, "not: [valid")
	base := GitReader{Root: root, Repository: "fixture", Revision: revision, ManifestPath: "checkout.yaml", Target: WorkloadTarget{Kind: "Deployment", Name: "checkout", Container: "checkout"}}
	wrong := base
	wrong.Revision = strings.Repeat("b", 40)
	if _, err := wrong.Read(); err == nil {
		t.Fatal("wrong revision accepted")
	}
	if _, err := base.Read(); err == nil {
		t.Fatal("malformed manifest accepted")
	}
	root, revision = fixtureRepo(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: other}\n")
	base.Root, base.Revision = root, revision
	if _, err := base.Read(); err == nil {
		t.Fatal("missing target accepted")
	}
}
