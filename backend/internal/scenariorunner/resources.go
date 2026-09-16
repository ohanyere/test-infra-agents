package scenariorunner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func httpProbe(path string) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromString("http"), Scheme: corev1.URISchemeHTTP}},
		InitialDelaySeconds: 2,
		PeriodSeconds:       5,
		TimeoutSeconds:      2,
		FailureThreshold:    3,
	}
}

func resourceLabels(identity kubeenv.RunIdentity, component string) map[string]string {
	return map[string]string{
		kubeenv.ManagedLabelKey:     "true",
		kubeenv.RunIDLabelKey:       identity.RunID,
		kubeenv.BatchIDLabelKey:     identity.BatchID,
		kubeenv.WorkspaceIDLabelKey: identity.WorkspaceID,
		kubeenv.ManagedByLabelKey:   "evalplane",
		ComponentLabelKey:           component,
	}
}

func verifyResourceOwnership(labels map[string]string, identity kubeenv.RunIdentity, component string) error {
	if labels[kubeenv.ManagedLabelKey] != "true" ||
		labels[kubeenv.RunIDLabelKey] != identity.RunID ||
		labels[kubeenv.BatchIDLabelKey] != identity.BatchID ||
		labels[kubeenv.WorkspaceIDLabelKey] != identity.WorkspaceID ||
		labels[ComponentLabelKey] != component {
		return ErrResourceOwnership
	}
	return nil
}

func conflict(name string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrResourceConflict, name, err)
}

func kubernetesError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%s: resource not found", operation)
	}
	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("%w: %s", ErrResourceConflict, operation)
	}
	if errors.Is(err, ErrResourceConflict) || errors.Is(err, ErrResourceOwnership) {
		return err
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func k6Script(profile TrafficSpikeProfile) string {
	stages := make([]string, 0, len(profile.Stages))
	for _, stage := range profile.Stages {
		stages = append(stages, fmt.Sprintf("{ duration: '%s', target: %d }", stage.Duration.String(), stage.Target))
	}
	return fmt.Sprintf(`import http from 'k6/http';
import { check } from 'k6';

export const options = {
  noConnectionReuse: true,
  stages: [%s],
};

export default function () {
  const response = http.get('http://%s');
  check(response, { 'status is 200': (r) => r.status === 200 });
}
`, strings.Join(stages, ", "), ReferenceServiceName)
}
