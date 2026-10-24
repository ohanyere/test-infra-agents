package kubeaudit

// CustomerActionTarget identifies the narrow Kubernetes mutation classes
// supported by the direct customer audit receiver. It does not establish a
// resulting state, public exposure, or agent identity.
func CustomerActionTarget(event Event) (kind, namespace string, ok bool) {
	if event.APIVersion != AuditAPIVersion || event.Kind != "Event" || event.Stage != "ResponseComplete" || event.AuditID == "" || event.ObjectRef.Name == "" || event.Response.Code < 200 || event.Response.Code >= 300 {
		return "", "", false
	}
	if event.Verb != "create" && event.Verb != "patch" && event.Verb != "update" && event.Verb != "delete" {
		return "", "", false
	}
	switch {
	case event.ObjectRef.APIGroup == "apps" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "deployments":
		return "Deployment", event.ObjectRef.Namespace, true
	case event.ObjectRef.APIGroup == "" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "services":
		return "Service", event.ObjectRef.Namespace, true
	case event.ObjectRef.APIGroup == "networking.k8s.io" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "ingresses":
		return "Ingress", event.ObjectRef.Namespace, true
	case event.ObjectRef.APIGroup == "networking.k8s.io" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "networkpolicies":
		return "NetworkPolicy", event.ObjectRef.Namespace, true
	case event.ObjectRef.APIGroup == "" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "namespaces":
		return "Namespace", event.ObjectRef.Name, true
	}
	return "", "", false
}
