package kubeenv

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type resourceBoundaries struct {
	quota          corev1.ResourceList
	defaults       corev1.ResourceList
	defaultRequest corev1.ResourceList
}

type Manager struct {
	client     kubernetes.Interface
	boundaries resourceBoundaries
}

func NewManager(client kubernetes.Interface) (*Manager, error) {
	return NewManagerWithConfig(client, DefaultBoundaryConfig())
}

func NewManagerWithConfig(client kubernetes.Interface, config BoundaryConfig) (*Manager, error) {
	if client == nil {
		return nil, errors.New("Kubernetes client is required")
	}
	parse := func(name, value string) (resource.Quantity, error) {
		quantity, err := resource.ParseQuantity(value)
		if err != nil {
			return resource.Quantity{}, fmt.Errorf("invalid %s boundary: %w", name, err)
		}
		return quantity, nil
	}
	values := make(map[string]resource.Quantity, 9)
	for name, value := range map[string]string{
		"pods": config.Pods, "requests.cpu": config.RequestsCPU, "limits.cpu": config.LimitsCPU,
		"requests.memory": config.RequestsMemory, "limits.memory": config.LimitsMemory,
		"default.cpu": config.DefaultCPU, "default.memory": config.DefaultMemory,
		"defaultRequest.cpu": config.DefaultRequestCPU, "defaultRequest.memory": config.DefaultRequestMemory,
	} {
		quantity, err := parse(name, value)
		if err != nil {
			return nil, err
		}
		values[name] = quantity
	}
	return &Manager{client: client, boundaries: resourceBoundaries{
		quota: corev1.ResourceList{
			corev1.ResourcePods:           values["pods"],
			corev1.ResourceRequestsCPU:    values["requests.cpu"],
			corev1.ResourceLimitsCPU:      values["limits.cpu"],
			corev1.ResourceRequestsMemory: values["requests.memory"],
			corev1.ResourceLimitsMemory:   values["limits.memory"],
		},
		defaults: corev1.ResourceList{
			corev1.ResourceCPU:    values["default.cpu"],
			corev1.ResourceMemory: values["default.memory"],
		},
		defaultRequest: corev1.ResourceList{
			corev1.ResourceCPU:    values["defaultRequest.cpu"],
			corev1.ResourceMemory: values["defaultRequest.memory"],
		},
	}}, nil
}

func (m *Manager) CreateEnvironment(ctx context.Context, identity RunIdentity) (Environment, error) {
	identity, err := ValidateRunIdentity(identity)
	if err != nil {
		return Environment{}, err
	}
	namespace, err := NamespaceForRun(identity.RunID)
	if err != nil {
		return Environment{}, err
	}
	labels := ownershipLabels(identity)
	created, err := m.client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: labels}}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = m.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if err == nil {
			err = verifyIdentity(created.Labels, identity)
		}
	}
	if err != nil {
		if errors.Is(err, ErrEnvironmentOwnership) {
			return Environment{}, fmt.Errorf("%w: namespace %s: %w", ErrEnvironmentConflict, namespace, err)
		}
		return Environment{}, mapKubernetesError("create environment namespace", err)
	}
	if err = m.ensureQuota(ctx, namespace, labels, identity); err != nil {
		return Environment{}, err
	}
	if err = m.ensureLimitRange(ctx, namespace, labels, identity); err != nil {
		return Environment{}, err
	}
	return environment(identity.RunID, namespace), nil
}

func (m *Manager) GetEnvironment(ctx context.Context, runID string) (Environment, error) {
	runID, err := ValidateRunID(runID)
	if err != nil {
		return Environment{}, err
	}
	namespace, err := NamespaceForRun(runID)
	if err != nil {
		return Environment{}, err
	}
	value, err := m.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return Environment{}, mapKubernetesError("get environment namespace", err)
	}
	if err = verifyRunOwnership(value.Labels, runID); err != nil {
		return Environment{}, err
	}
	return environment(runID, namespace), nil
}

func (m *Manager) DeleteEnvironment(ctx context.Context, runID string) error {
	runID, err := ValidateRunID(runID)
	if err != nil {
		return err
	}
	namespace, err := NamespaceForRun(runID)
	if err != nil {
		return err
	}
	value, err := m.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return mapKubernetesError("get environment namespace before deletion", err)
	}
	if err = verifyRunOwnership(value.Labels, runID); err != nil {
		return err
	}
	if err = m.client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}); apierrors.IsNotFound(err) {
		return nil
	}
	return mapKubernetesError("delete environment namespace", err)
}

// DeleteOwnedEnvironment validates the full workspace/batch/run identity and uses
// a UID precondition so a replaced namespace cannot be deleted by a stale worker.
func (m *Manager) DeleteOwnedEnvironment(ctx context.Context, identity RunIdentity) error {
	identity, err := ValidateRunIdentity(identity)
	if err != nil {
		return err
	}
	namespace, err := NamespaceForRun(identity.RunID)
	if err != nil {
		return err
	}
	value, err := m.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = verifyIdentity(value.Labels, identity); err != nil {
		return err
	}
	uid := value.UID
	err = m.client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *Manager) ensureQuota(ctx context.Context, namespace string, labels map[string]string, identity RunIdentity) error {
	quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: quotaName, Namespace: namespace, Labels: labels}, Spec: corev1.ResourceQuotaSpec{Hard: m.boundaries.quota.DeepCopy()}}
	existing, err := m.client.CoreV1().ResourceQuotas(namespace).Create(ctx, quota, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, err = m.client.CoreV1().ResourceQuotas(namespace).Get(ctx, quotaName, metav1.GetOptions{})
		if err == nil {
			err = verifyIdentity(existing.Labels, identity)
		}
	}
	if err != nil {
		return mapKubernetesError("ensure environment resource quota", err)
	}
	return nil
}

func (m *Manager) ensureLimitRange(ctx context.Context, namespace string, labels map[string]string, identity RunIdentity) error {
	limits := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: limitRangeName, Namespace: namespace, Labels: labels}, Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, Default: m.boundaries.defaults.DeepCopy(), DefaultRequest: m.boundaries.defaultRequest.DeepCopy()}}}}
	existing, err := m.client.CoreV1().LimitRanges(namespace).Create(ctx, limits, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, err = m.client.CoreV1().LimitRanges(namespace).Get(ctx, limitRangeName, metav1.GetOptions{})
		if err == nil {
			err = verifyIdentity(existing.Labels, identity)
		}
	}
	if err != nil {
		return mapKubernetesError("ensure environment limit range", err)
	}
	return nil
}

func ownershipLabels(identity RunIdentity) map[string]string {
	return map[string]string{
		ManagedLabelKey:     "true",
		RunIDLabelKey:       identity.RunID,
		BatchIDLabelKey:     identity.BatchID,
		WorkspaceIDLabelKey: identity.WorkspaceID,
		ManagedByLabelKey:   "evalplane",
	}
}

func verifyIdentity(labels map[string]string, identity RunIdentity) error {
	if err := verifyRunOwnership(labels, identity.RunID); err != nil {
		return err
	}
	if labels[BatchIDLabelKey] != identity.BatchID || labels[WorkspaceIDLabelKey] != identity.WorkspaceID {
		return ErrEnvironmentOwnership
	}
	return nil
}

func verifyRunOwnership(labels map[string]string, runID string) error {
	if labels[ManagedLabelKey] != "true" || labels[RunIDLabelKey] != runID {
		return ErrEnvironmentOwnership
	}
	return nil
}

func environment(runID, namespace string) Environment {
	return Environment{RunID: runID, Namespace: namespace, Reference: EnvironmentReference(namespace)}
}

func mapKubernetesError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		return ErrEnvironmentNotFound
	}
	if apierrors.IsAlreadyExists(err) {
		return ErrEnvironmentConflict
	}
	if errors.Is(err, ErrEnvironmentOwnership) || errors.Is(err, ErrEnvironmentConflict) || errors.Is(err, ErrEnvironmentNotFound) {
		return err
	}
	return fmt.Errorf("%s: %w", operation, err)
}
