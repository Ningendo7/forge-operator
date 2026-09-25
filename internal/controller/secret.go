package controller

import (
	"context"
	"fmt"
	"strings"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/akamaiobjstr"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// SecretRoleLabel distinguishes the app-managed Secret from the storage credentials
// Secret, since both carry the same "app" label and would otherwise be indistinguishable
// when cleaning up stale/renamed resources. Exported so cmd/main.go can scope the
// manager's Secret cache to exactly the Secrets that carry it -- see the Cache/Client
// options built around it there.
const SecretRoleLabel = "forge.ningendo7.github.io/secret-role"

const secretRoleStorage = "storage"

// desiredStorage builds the operator-managed storage credentials Secret.
// akamaiCreds is passed explicitly by the caller (never read from
// application.Status) because the raw access/secret key pair must never be
// persisted anywhere other than this Secret; see AkamaiStorageStatus's doc
// comment for why.
func (r *ApplicationReconciler) desiredStorage(
	application *forgev1alpha1.Application,
	akamaiCreds *akamaiobjstr.StorageResult,
) *corev1.Secret {

	if application.Spec.Storage == nil {
		return nil
	}

	name := naming.StorageSecret(application)

	secretData := map[string]string{
		"provider": string(application.Spec.Storage.Provider),
		"bucket":   application.Spec.Storage.Bucket,
		"region":   application.Spec.Storage.Region,
		"endpoint": application.Spec.Storage.Endpoint,
	}

	// Inject AWS IRSA Role if present in status
	if application.Status.Storage != nil && application.Status.Storage.AWS != nil {
		secretData["role_arn"] = application.Status.Storage.AWS.RoleARN
	}

	// Inject Akamai Object Storage credentials, supplied directly by the caller.
	if akamaiCreds != nil {
		secretData["access_key"] = akamaiCreds.AccessKey

		// Avoid overwriting with empty value if Akamai didn't reissue one.
		if akamaiCreds.SecretKey != "" {
			secretData["secret_key"] = akamaiCreds.SecretKey
		}

		secretData["endpoint"] = akamaiCreds.Endpoint
		// "endpoint" above is kept as the bucket-prefixed hostname
		// (bucket.cluster.linodeobjects.com) for backward compatibility.
		// endpoint_url strips that bucket prefix back off: this operator's
		// own S3 client (s3ClientFor in the Akamai package) uses path-style
		// addressing (bare cluster host as BaseEndpoint, bucket passed
		// explicitly per request), so handing a real SDK the bucket-prefixed
		// host would double up the bucket reference once it also passes a
		// Bucket parameter.
		endpointHost := strings.TrimPrefix(akamaiCreds.Endpoint, application.Spec.Storage.Bucket+".")
		secretData["endpoint_url"] = "https://" + endpointHost
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    map[string]string{appLabelKey: application.Name, SecretRoleLabel: secretRoleStorage},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: secretData,
	}
}

// deleteStaleSecrets deletes Secrets of the given role owned by application other than
// keepName, so a rename or full disable can never orphan the previous one.
func (r *ApplicationReconciler) deleteStaleSecrets(
	ctx context.Context,
	application *forgev1alpha1.Application,
	role string,
	keepName string,
) error {
	logger := logf.FromContext(ctx)

	var list corev1.SecretList
	if err := r.List(ctx, &list, client.InNamespace(application.Namespace), client.MatchingLabels{appLabelKey: application.Name, SecretRoleLabel: role}); err != nil {
		return fmt.Errorf("failed to list Secrets for cleanup: %w", err)
	}

	for i := range list.Items {
		sec := &list.Items[i]
		if sec.Name == keepName || !metav1.IsControlledBy(sec, application) {
			continue
		}
		if err := r.Delete(ctx, sec); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete stale Secret %s: %w", sec.Name, err)
		}
		logger.Info("Deleted stale Secret", "name", sec.Name, "role", role)
	}
	return nil
}

func (r *ApplicationReconciler) reconcileStorageSecret(
	ctx context.Context,
	application *forgev1alpha1.Application,
	akamaiCreds *akamaiobjstr.StorageResult,
) error {

	logger := logf.FromContext(ctx)

	if application.Spec.Storage == nil {
		return r.deleteStaleSecrets(ctx, application, secretRoleStorage, "")
	}

	logger.Info("Reconciling Storage Secret")

	desired := r.desiredStorage(application, akamaiCreds)
	if desired == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for Storage Secret: %w", err)
	}

	err := r.Patch(
		ctx,
		desired,
		client.Apply, //nolint:staticcheck // SSA patch via client.Apply is the standard controller-runtime pattern
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply Storage Secret", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply Storage Secret: %w", err)
	}

	if err := r.deleteStaleSecrets(ctx, application, secretRoleStorage, desired.Name); err != nil {
		return fmt.Errorf("failed to clean up stale Storage Secret: %w", err)
	}

	logger.Info("Successfully reconciled Storage Secret", "name", desired.Name)
	return nil
}

// findApplicationsForSecret maps a Secret event to any Application whose
// storage credentials it holds -- spec.storage.aws.credentialsSecretRef (AWS
// static creds) or the Akamai token Secret
// (spec.storage.akamai.accessKeySecretRef, naming.AkamaiTokenSecret's
// default).
func (r *ApplicationReconciler) findApplicationsForSecret(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	var appList forgev1alpha1.ApplicationList
	if err := r.List(ctx, &appList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request

	for _, app := range appList.Items {
		if app.Spec.Storage == nil {
			continue
		}
		referenced := app.Spec.Storage.SecretName == secret.Name ||
			(app.Spec.Storage.Provider == forgev1alpha1.ProviderAkamaiObjectStorage &&
				naming.AkamaiTokenSecret(&app) == secret.Name) ||
			(app.Spec.Storage.Provider == forgev1alpha1.ProviderAWSS3 &&
				app.Spec.Storage.AWS != nil && app.Spec.Storage.AWS.CredentialsSecretRef == secret.Name)
		if referenced {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      app.Name,
					Namespace: app.Namespace,
				},
			})
		}
	}

	return requests
}
