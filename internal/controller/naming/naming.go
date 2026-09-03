// Package naming is the single source of truth for names of resources the
// Application controller creates, so builders and readers never disagree.
package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

// AdoptBucketAnnotation, when set to "true" on an Application, tells the
// ownership check (claimOrVerifyOwnership in both the s3 and
// Akamai-Obj-Str packages) to overwrite a mismatched ownership tag/marker
// instead of rejecting the bucket as foreign. This is the deliberate,
// explicit opt-in for taking over a bucket a *different* Application
// previously owned -- most commonly one left behind by
// spec.storage.deletionPolicy: Retain. Shared between both provider
// packages so the exact annotation key can never drift between them.
const AdoptBucketAnnotation = "forge-operator.ningendo7.github.io/adopt-bucket"

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

// CloudResourceName builds a name for an external cloud resource (IAM role,
// Object Storage access key label, etc.) that must be unique across the
// whole cloud account, not just within this Kubernetes cluster. Unlike
// Kubernetes objects -- which are naturally namespaced by the API server --
// these live in a single flat namespace on the provider's side, so two
// Applications with the same name in different Kubernetes namespaces would
// collide on an identically-named cloud resource unless the namespace is
// folded into the name here.
//
// parts are joined with "-". If the result would exceed maxLen (providers
// often impose one -- e.g. AWS IAM role names cap at 64 characters), it's
// truncated with a short content hash appended instead, so it stays valid
// while remaining unique rather than silently colliding again.
func CloudResourceName(parts []string, maxLen int) string {
	full := strings.Join(parts, "-")
	if len(full) <= maxLen {
		return full
	}

	sum := sha256.Sum256([]byte(full))
	suffix := "-" + hex.EncodeToString(sum[:])[:8]

	keep := maxLen - len(suffix)
	if keep < 0 {
		keep = 0
	}
	if keep > len(full) {
		keep = len(full)
	}
	return full[:keep] + suffix
}
