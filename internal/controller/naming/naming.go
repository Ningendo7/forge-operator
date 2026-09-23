// Package naming is the single source of truth for names of resources the
// Application controller creates, so builders and readers never disagree.
package naming

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
//
// On its own this only ever authorizes adoption within the same namespace
// as the previous owner; taking a bucket across namespaces additionally
// requires the previous owner's namespace to carry
// AllowBucketAdoptionFromAnnotation (see EvaluateBucketAdoption).
const AdoptBucketAnnotation = "forge-operator.ningendo7.github.io/adopt-bucket"

// StorageOwnershipIDAnnotation records a random, operator-generated identity
// for this Application's storage ownership (see storage.go's
// ensureStorageOwnershipID, which generates and persists it once and never
// regenerates one that's already present). Neither of the two identifiers
// ownership already relies on survives a Velero restore or cluster
// migration: metadata.uid is server-assigned and can never be preserved
// across recreation, and Status.Storage.CreatedAt lives on a subresource
// that restore tooling generally can't populate via a plain Create call.
// This annotation is ordinary object metadata with no such gate, so both
// naturally carry it forward -- letting a genuinely restored Application
// reclaim its own bucket automatically (see claimOrVerifyOwnership in the
// s3 and Akamai-Obj-Str packages) without a human setting
// AdoptBucketAnnotation by hand, while a merely name-colliding new
// Application -- which was never handed this value -- still can't.
const StorageOwnershipIDAnnotation = "forge-operator.ningendo7.github.io/storage-ownership-id"

// AdoptBucketAnnotationValue is the value AdoptBucketAnnotation must be set
// to for it to take effect (kept alongside the key so both packages read it
// from one shared source, matching AdoptBucketAnnotation's own reasoning).
const AdoptBucketAnnotationValue = "true"

// ApplicationExistsWithUID reports whether any Application in the cluster
// currently has the given UID. The ownership tag/marker AdoptBucketAnnotation
// overwrites only ever stores a bare UID with no namespace/name, and
// Kubernetes has no "get by UID" lookup, so listing and scanning is the only
// way to confirm the previous owner is actually gone rather than assuming it
// from the annotation alone.
//
// Deliberately checks existence, not readiness: an Application mid transient
// failure must not become adoptable just because it's temporarily not Ready.
//
// Requires cluster-wide list/watch on Applications (already granted under
// this chart's default ClusterRole RBAC). Under rbac.namespaced: true (a
// Role instead), this only sees Applications in the operator's own
// namespace or fails with Forbidden -- callers MUST treat any non-nil error
// as "cannot confirm the previous owner is gone" and refuse the adoption,
// never as "must be gone since we couldn't find it".
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

// AllowBucketAdoptionFromAnnotation, set on a Namespace object, grants
// Applications in the named namespace(s) (comma-separated, or "*" for any)
// permission to adopt buckets left behind by Applications that lived in
// this namespace. Lives on the Namespace, not the adopting Application:
// editing a Namespace typically requires separate, more-privileged RBAC
// than editing Applications inside one, so an ordinary tenant can't grant
// this to themselves.
const AllowBucketAdoptionFromAnnotation = "forge.ningendo7.github.io/allow-bucket-adoption-from"

// CrossNamespaceAdoptionAllowed reports whether ownerNamespace's Namespace
// object grants adoptingNamespace permission to adopt buckets it left
// behind, via AllowBucketAdoptionFromAnnotation.
func CrossNamespaceAdoptionAllowed(ctx context.Context, c client.Client, ownerNamespace, adoptingNamespace string) (bool, error) {
	var ns corev1.Namespace
	if err := c.Get(ctx, types.NamespacedName{Name: ownerNamespace}, &ns); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get namespace %q while checking cross-namespace bucket adoption grant: %w", ownerNamespace, err)
	}

	grant := ns.Annotations[AllowBucketAdoptionFromAnnotation]
	if grant == "" {
		return false, nil
	}
	if grant == "*" {
		return true, nil
	}
	for _, allowed := range strings.Split(grant, ",") {
		if strings.TrimSpace(allowed) == adoptingNamespace {
			return true, nil
		}
	}
	return false, nil
}

// EvaluateBucketAdoption decides whether adoptingNamespace may take over a
// bucket whose ownership tag/marker names ownerUID. ownerNamespace is the
// namespace recorded alongside that UID at tag/marker-write time -- "" means
// the bucket was tagged before namespace-scoped ownership shipped and its
// owner's namespace is unknown. Returns nil to allow adoption, or an error
// explaining the refusal otherwise.
//
// Same-namespace adoption is allowed automatically once the previous owner
// is confirmed gone. Cross-namespace adoption additionally requires an
// explicit grant on the previous owner's Namespace object (see
// AllowBucketAdoptionFromAnnotation). Buckets with no owner namespace
// recorded refuse cross-namespace adoption unconditionally until re-tagged
// -- there's nothing to check a grant against.
func EvaluateBucketAdoption(ctx context.Context, c client.Client, ownerUID types.UID, ownerNamespace, adoptingNamespace string) error {
	exists, err := ApplicationExistsWithUID(ctx, c, ownerUID)
	if err != nil {
		return fmt.Errorf("could not confirm the previous owner no longer exists: %w", err)
	}
	if exists {
		return errors.New("the Application that currently owns this bucket still exists")
	}

	if ownerNamespace == "" {
		return fmt.Errorf("this bucket's ownership was recorded before namespace-scoped adoption shipped; re-tag it manually before adopting")
	}
	if ownerNamespace == adoptingNamespace {
		return nil
	}

	allowed, err := CrossNamespaceAdoptionAllowed(ctx, c, ownerNamespace, adoptingNamespace)
	if err != nil {
		return fmt.Errorf("could not verify cross-namespace bucket adoption grant: %w", err)
	}
	if !allowed {
		return fmt.Errorf("cross-namespace adoption (%s -> %s) requires namespace %q to carry the %s annotation naming %q",
			ownerNamespace, adoptingNamespace, ownerNamespace, AllowBucketAdoptionFromAnnotation, adoptingNamespace)
	}
	return nil
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
