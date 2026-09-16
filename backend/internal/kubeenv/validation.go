package kubeenv

import (
	"fmt"
	"regexp"
	"strings"

	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidateRunIdentity(identity RunIdentity) (RunIdentity, error) {
	identity.RunID = strings.ToLower(strings.TrimSpace(identity.RunID))
	identity.BatchID = strings.ToLower(strings.TrimSpace(identity.BatchID))
	identity.WorkspaceID = strings.ToLower(strings.TrimSpace(identity.WorkspaceID))
	for name, value := range map[string]string{"run ID": identity.RunID, "batch ID": identity.BatchID, "workspace ID": identity.WorkspaceID} {
		if !uuidPattern.MatchString(value) {
			return identity, fmt.Errorf("%s must be a valid UUID", name)
		}
	}
	return identity, nil
}

func ValidateRunID(runID string) (string, error) {
	runID = strings.ToLower(strings.TrimSpace(runID))
	if !uuidPattern.MatchString(runID) {
		return "", fmt.Errorf("run ID must be a valid UUID")
	}
	return runID, nil
}

func NamespaceForRun(runID string) (string, error) {
	runID, err := ValidateRunID(runID)
	if err != nil {
		return "", err
	}
	name := "eval-" + strings.ReplaceAll(runID, "-", "")
	if problems := utilvalidation.IsDNS1123Label(name); len(problems) > 0 {
		return "", fmt.Errorf("derived namespace is invalid: %s", strings.Join(problems, "; "))
	}
	return name, nil
}

func EnvironmentReference(namespace string) string {
	return "k8s://" + namespace
}
