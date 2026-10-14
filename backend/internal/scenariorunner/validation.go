package scenariorunner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
)

func validateConfig(config Config) error {
	if strings.TrimSpace(config.ReferenceImage) == "" || strings.HasSuffix(config.ReferenceImage, ":latest") {
		return errors.New("reference image must use an explicit non-latest tag")
	}
	if strings.TrimSpace(config.TrafficImage) == "" || strings.HasSuffix(config.TrafficImage, ":latest") {
		return errors.New("traffic image must use an explicit non-latest tag")
	}
	if config.ReferenceReplicas < 1 || config.ReferenceReadyTimeout <= 0 || config.TrafficTimeout <= 0 || config.PollInterval <= 0 {
		return errors.New("replicas and runner durations must be positive")
	}
	return nil
}

func validateEnvironment(value EnvironmentContext) (EnvironmentContext, error) {
	identity, err := kubeenv.ValidateRunIdentity(value.Identity)
	if err != nil {
		return value, err
	}
	expectedNamespace, err := kubeenv.NamespaceForRun(identity.RunID)
	if err != nil {
		return value, err
	}
	if value.Environment.RunID != identity.RunID || value.Environment.Namespace != expectedNamespace || value.Environment.Reference != kubeenv.EnvironmentReference(expectedNamespace) {
		return value, kubeenv.ErrEnvironmentOwnership
	}
	value.Identity = identity
	return value, nil
}

func validateProfile(profile TrafficSpikeProfile) error {
	if len(profile.Stages) == 0 {
		return errors.New("traffic spike profile requires at least one stage")
	}
	if profile.StartVUs != nil && (*profile.StartVUs < 0 || *profile.StartVUs > 1000) {
		return errors.New("traffic spike start VUs must be between 0 and 1000")
	}
	for index, stage := range profile.Stages {
		if stage.Target < 0 || stage.Duration <= 0 {
			return fmt.Errorf("traffic stage %d must have a non-negative target and positive duration", index+1)
		}
	}
	return nil
}

func profileDuration(profile TrafficSpikeProfile) time.Duration {
	var total time.Duration
	for _, stage := range profile.Stages {
		total += stage.Duration
	}
	return total
}
