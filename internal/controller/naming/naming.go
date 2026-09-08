// Package naming is the single source of truth for names of resources the
// Application controller creates, so builders and readers never disagree.
package naming

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// AdoptBucketAnnotationValue is the value AdoptBucketAnnotation must be set
// to for it to take effect (kept alongside the key so both packages read it
// from one shared source, matching AdoptBucketAnnotation's own reasoning).
const AdoptBucketAnnotationValue = "true"

// ApplicationExistsWithUID reports whether any Application in the cluster
// currently has the given UID. AdoptBucketAnnotation's whole premise is
// taking over a bucket left behind by an Application that's genuinely
// gone (most commonly one retained via spec.storage.deletionPolicy:
// Retain) -- but the ownership tag/marker it's overwriting only ever
// stores a bare UID, with no namespace/name to check directly, and
// Kubernetes has no "get by UID" lookup. Listing every Application and
// scanning for a match is the only way to answer "is the previous owner
// actually gone" rather than just assuming it because the annotation was
// set, which is what let a live, still-in-use bucket be silently taken
// over from its still-running owner before this existed.
//
// Deliberately checks existence, not readiness: an Application mid
// transient failure (a flaky dependency, a brief misconfiguration --
// anything recoverable) must not become adoptable by anyone with the
// annotation just because it's temporarily not Ready. Only a Kubernetes
// object that's actually gone from the API server counts as "gone" here.
//
// Requires cluster-wide list/watch on Applications -- already granted
// unconditionally in this chart's default (ClusterRole) RBAC mode, since
// any cluster-scoped operator needs it to reconcile every Application in
// every namespace regardless of this function. If this chart is instead
// deployed with rbac.namespaced: true (a Role, not a ClusterRole), this
// List call will only ever see Applications in the operator's own
// namespace, or fail outright with Forbidden -- callers MUST treat any
// non-nil error here as "cannot confirm the previous owner is gone" and
// refuse the adoption, not as "must be gone since we couldn't find it".
// Silently permitting a live takeover under reduced RBAC would be far
// worse than adopt-bucket simply not working there.
func ApplicationExistsWithUID(ctx context.Context, c client.Client, uid types.UID) (bool, error) {
	if uid == "" {
		return false, nil
	}

	var list forgev1alpha1.ApplicationList
	if err := c.List(ctx, &list); err != nil {
		return false, fmt.Errorf("failed to list Applications while checking for UID %s: %w", uid, err)
	}

	for i := range list.Items {
		if list.Items[i].UID == uid {
			return true, nil
		}
	}
	return false, nil
}

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

// AppConfigMap returns the name of the operator-managed ConfigMap, honoring
// spec.config.name when set. Exported (rather than kept private to the
// configmap reconciler) so the webhook can compute the same name a removed
// spec.config would have used, to detect spec.container.configMapName still
// dangling a reference to it.
func AppConfigMap(application *forgev1alpha1.Application) string {
	if application.Spec.ConfigMap != nil && application.Spec.ConfigMap.Name != "" {
		return application.Spec.ConfigMap.Name
	}
	return application.Name + "-config"
}

// AppSecret returns the name of the operator-managed app Secret, honoring
// spec.secret.name when set. Exported for the same reason as AppConfigMap.
func AppSecret(application *forgev1alpha1.Application) string {
	if application.Spec.Secret != nil && application.Spec.Secret.Name != "" {
		return application.Spec.Secret.Name
	}
	return application.Name + "-secret"
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

	keep := min(max(maxLen-len(suffix), 0), len(full))
	return full[:keep] + suffix
}
