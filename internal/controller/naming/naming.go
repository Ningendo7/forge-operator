// Package naming is the single source of truth for names of resources the
// Application controller creates, so builders and readers never disagree.
package naming

import (
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

// Service returns the name of the Application's Service.
func Service(application *forgev1alpha1.Application) string {
	return application.Name
}

// Deployment returns the name of the Application's Deployment.
func Deployment(application *forgev1alpha1.Application) string {
	return application.Name + "-deployment"
}

// Ingress returns the name of the Application's Ingress.
func Ingress(application *forgev1alpha1.Application) string {
	return application.Name
}

// HPA returns the name of the Application's HorizontalPodAutoscaler.
func HPA(application *forgev1alpha1.Application) string {
	return application.Name + "-hpa"
}

// PDB returns the name of the Application's PodDisruptionBudget.
func PDB(application *forgev1alpha1.Application) string {
	return application.Name + "-pdb"
}

// StorageSecret returns the name of the operator-managed Secret that holds
// generated object storage credentials (bucket access/secret key, IRSA role
// ARN, etc.), honoring spec.storage.secretName when set.
func StorageSecret(application *forgev1alpha1.Application) string {
	if application.Spec.Storage != nil && application.Spec.Storage.SecretName != "" {
		return application.Spec.Storage.SecretName
	}
	return application.Name + "-storage"
}

// AkamaiTokenSecret returns the name of the user-supplied Secret holding the
// Akamai/Linode API token (key: apiToken), honoring
// spec.storage.akamai.accessKeySecretRef when set. This is deliberately a
// different default name than StorageSecret: that Secret is the operator's
// own generated output, owned and overwritten by the controller, while this
// one is a user-managed input credential — reusing the same name would mean
// the operator's writes and the controller-owned delete-on-cascade lifecycle
// would apply to the user's manually-created token Secret too.
func AkamaiTokenSecret(application *forgev1alpha1.Application) string {
	if application.Spec.Storage != nil && application.Spec.Storage.Akamai != nil &&
		application.Spec.Storage.Akamai.AccessKeySecretRef != "" {
		return application.Spec.Storage.Akamai.AccessKeySecretRef
	}
	return application.Name + "-akamai-token"
}
