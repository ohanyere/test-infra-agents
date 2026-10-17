package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

func main() {
	namespace := flag.String("namespace", "", "expected target namespace")
	name := flag.String("name", "", "expected Deployment name")
	verb := flag.String("verb", "", "expected API verb")
	principal := flag.String("principal", "", "expected authenticated Kubernetes principal")
	flag.Parse()
	if *namespace == "" || *name == "" || *verb == "" || *principal == "" || flag.NArg() != 0 {
		fatal("expected namespace, name, verb, and principal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	selected, err := kubeaudit.WaitForMutation(ctx, *namespace, *name, *verb, *principal)
	if err != nil {
		fatal(err.Error())
	}
	event, source, ok := selected.Snapshot()
	if !ok {
		fatal("audit event did not pass runner source binding")
	}
	result := struct {
		Status string                   `json:"status"`
		Source kubeaudit.SourceIdentity `json:"source"`
		Event  kubeaudit.Event          `json:"event"`
		Notice string                   `json:"notice"`
	}{"runner_bound_audit_acquired", source, event, "runner-bound Kind test evidence only; no production customer source authentication"}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal(fmt.Sprintf("encode proof result: %v", err))
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
