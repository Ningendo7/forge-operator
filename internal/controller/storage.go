package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/akamaiobjstr"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
)

// storageReconcileTimeout bounds each storage provisioning attempt so a hung
// cloud call can't block every other Application's reconcile indefinitely.
const storageReconcileTimeout = 90 * time.Second

// logStorageStatusUpdateError logs a best-effort status write failure without
// masking the underlying storage error that triggered it.
func logStorageStatusUpdateError(ctx context.Context, err error) {
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to update Application storage status")
	}
}

// retryStatusUpdate persists application.Status via Status().Update(),
// retrying with a freshly-fetched copy on a resourceVersion conflict rather
// than surfacing it as a hard Reconcile error. Mirrors
// status.StatusManager.UpdateStatus's own fix for the identical class of bug
// (see its doc comment for the mechanism), generalized here for every place
// in this package that writes application.Status directly instead of going
// through StatusManager. Unlike StatusManager, this re-fetches via the same
// cached client rather than a dedicated uncached APIReader -- RetryOnConflict's
// own backoff between attempts gives the cache time to catch up instead.
func retryStatusUpdate(ctx context.Context, c client.Client, application *forgev1alpha1.Application) error {
	desiredStatus := application.Status
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		application.Status = desiredStatus
		updateErr := c.Status().Update(ctx, application)
		if updateErr == nil {
			return nil
		}
		if !apierrors.IsConflict(updateErr) {
			return updateErr
		}
		if getErr := c.Get(ctx, client.ObjectKeyFromObject(application), application); getErr != nil {
			return getErr
		}
		return updateErr
	})
}

// s3StorageManager and akamaiStorageManager are the minimal surfaces reconcileAWSStorage
// and reconcileAkamaiStorage depend on, so tests can substitute a fake manager instead of
// standing up real AWS/Akamai clients.
type s3StorageManager interface {
	ReconcileBucket(ctx context.Context) (*s3storage.StorageResult, error)
}

type akamaiStorageManager interface {
	ReconcileBucket(ctx context.Context) (*akamaiobjstr.StorageResult, error)
}

// newS3StorageManager and newAkamaiStorageManager are var-bound constructors so tests
// can swap them out; production code always uses the real provider packages.
var newS3StorageManager = func(
	ctx context.Context,
	c client.Client,
	application *forgev1alpha1.Application,
	serviceAccountName string,
	oidcProviderARN string,
	oidcProviderURL string,
	permissionsBoundaryARN string,
	recordCreated func(ctx context.Context) error,
	s3Limiter *rate.Limiter,
	iamLimiter *rate.Limiter,
) (s3StorageManager, error) {
	return s3storage.NewManager(
		ctx,
		c,
		application,
		serviceAccountName,
		oidcProviderARN,
		oidcProviderURL,
		permissionsBoundaryARN,
		recordCreated,
		s3Limiter,
		iamLimiter,
	)
}

var newAkamaiStorageManager = func(
	ctx context.Context,
	c client.Client,
	application *forgev1alpha1.Application,
	defaultRegion string,
	recordCreated func(ctx context.Context) error,
	accountLimiter *rate.Limiter,
	objectLimiter *rate.Limiter,
) (akamaiStorageManager, error) {
	return akamaiobjstr.NewManager(
		ctx,
		c,
		application,
		defaultRegion,
		recordCreated,
		accountLimiter,
		objectLimiter,
	)
}

func (r *ApplicationReconciler) reconcileStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	// If storage spec is nil, clean up any previously-provisioned cloud
	// resource (not just the credentials Secret) before returning --
	// otherwise removing spec.storage from an Application would silently
	// orphan its bucket.
	if application.Spec.Storage == nil {
		if application.Status.Storage != nil {
			if err := r.cleanupPreviousStorage(ctx, application, application.Status.Storage); err != nil {
				return err
			}
		}
		return r.reconcileStorageSecret(ctx, application, nil)
	}

	// Defense against a race the immutability webhook can't fully close:
	// validateStorageIdentityImmutable only compares each update against its
	// immediate predecessor, so a rapid nil -> different-identity sequence
	// (removed, then set again to a new provider/bucket/region as two
	// separate updates) can pass admission even though the overall change is
	// exactly what that check blocks. If the workqueue coalesces those two
	// updates into one reconcile, this method would jump straight from the
	// old identity to the new one and never observe the nil state in
	// between, silently orphaning whatever Status.Storage still remembers.
	// Detected here by comparing the identity Status.Storage last recorded
	// against what Spec.Storage names now; a mismatch means the target
	// changed out from under this reconcile, so the old one is cleaned up
	// first, same as an explicit nil transition would.
	if oldStorage := application.Status.Storage; oldStorage != nil &&
		(oldStorage.Provider != application.Spec.Storage.Provider ||
			oldStorage.Bucket != application.Spec.Storage.Bucket ||
			oldStorage.Region != application.Spec.Storage.Region) {
		if err := r.cleanupPreviousStorage(ctx, application, oldStorage); err != nil {
			return err
		}
	}

	if err := r.ensureStorageOwnershipID(ctx, application); err != nil {
		return fmt.Errorf("failed to ensure storage ownership ID: %w", err)
	}

	// akamaiCreds is only ever local to this call -- must never be assigned
	// to application.Status (see AkamaiStorageStatus's doc comment), which
	// is far more widely readable than the storage Secret it ends up in.
	var akamaiCreds *akamaiobjstr.StorageResult
	switch application.Spec.Storage.Provider {
	case forgev1alpha1.ProviderAWSS3:
		if err := r.reconcileAWSStorage(ctx, application); err != nil {
			return fmt.Errorf("failed to reconcile AWS storage: %w", err)
		}
	case forgev1alpha1.ProviderAkamaiObjectStorage:
		creds, err := r.reconcileAkamaiStorage(ctx, application)
		if err != nil {
			return fmt.Errorf("failed to reconcile Akamai storage: %w", err)
		}
		akamaiCreds = creds

	default:
		err := fmt.Errorf("unsupported storage provider: %s", application.Spec.Storage.Provider)
		storagestatus.SetNotReady(application, err)
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))
		return err
	}

	if err := r.reconcileStorageSecret(ctx, application, akamaiCreds); err != nil {
		return fmt.Errorf("failed to reconcile storage secret: %w", err)
	}

	return nil
}

// ensureStorageOwnershipID makes sure application carries
// naming.StorageOwnershipIDAnnotation before any provider package reads it
// off application.Annotations -- generating and persisting a fresh one on
// first use, never regenerating one that's already there. A deliberate,
// narrowly-scoped SSA patch (just the one annotation, not the full in-memory
// application) rather than a Status write: see
// naming.StorageOwnershipIDAnnotation's own doc comment for why this has to
// live in ordinary metadata rather than a subresource.
func (r *ApplicationReconciler) ensureStorageOwnershipID(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {
	if application.Annotations[naming.StorageOwnershipIDAnnotation] != "" {
		return nil
	}

	ownershipID := uuid.NewString()

	patch := &forgev1alpha1.Application{
		TypeMeta: metav1.TypeMeta{
			Kind:       applicationKind,
			APIVersion: forgev1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        application.Name,
			Namespace:   application.Namespace,
			Annotations: map[string]string{naming.StorageOwnershipIDAnnotation: ownershipID},
		},
	}

	if err := r.Patch(
		ctx,
		patch,
		client.Apply, //nolint:staticcheck // SSA patch via client.Apply is the standard controller-runtime pattern
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to persist storage ownership ID: %w", err)
	}

	if application.Annotations == nil {
		application.Annotations = map[string]string{}
	}
	application.Annotations[naming.StorageOwnershipIDAnnotation] = ownershipID

	return nil
}

// cleanupPreviousStorage deletes (or retains, per its own recorded
// DeletionPolicy) the cloud resource oldStorage describes, then clears
// Status.Storage and the StorageReady condition. Shared by
// reconcileStorage's two call sites: spec.storage removed outright, and
// spec.storage replaced with a different identity underneath an in-flight
// reconcile.
//
// oldStorage, not application.Spec.Storage, is what gets cleaned up -- via a
// throwaway DeepCopy with Spec.Storage overwritten to match oldStorage's
// identity, since application.Spec.Storage may already name the new target.
//
// The StorageReady condition is removed entirely rather than left at
// whatever finalizeApplication last set it to ("cleanup in progress" or a
// terminal failure), since here the Application lives on and a stuck,
// misleading condition would otherwise persist -- matching the condition an
// Application that never had storage configured shows: none at all.
func (r *ApplicationReconciler) cleanupPreviousStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
	oldStorage *forgev1alpha1.StorageStatus,
) error {
	cleanupSnapshot := application.DeepCopy()
	cleanupSnapshot.Spec.Storage = storageSpecFromStatus(oldStorage)
	if err := r.finalizeApplication(ctx, cleanupSnapshot); err != nil {
		return fmt.Errorf("failed to clean up previously provisioned storage: %w", err)
	}

	application.Status.Storage = nil
	apimeta.RemoveStatusCondition(&application.Status.Conditions, storagestatus.StorageReady)
	if err := retryStatusUpdate(ctx, r.Client, application); err != nil {
		return fmt.Errorf("failed to clear storage status after cleanup: %w", err)
	}
	return nil
}

func (r *ApplicationReconciler) reconcileAWSStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (err error) {

	provider := string(forgev1alpha1.ProviderAWSS3)

	// Child span of the "Reconcile" root span; every AWS call underneath
	// (via the otelaws middleware in the s3 package's NewManager) nests
	// under this in turn, since cloudCtx below derives from this ctx.
	ctx, span := forgemetrics.Tracer().Start(ctx, "reconcileAWSStorage",
		trace.WithAttributes(
			attribute.String("forge.storage.provider", provider),
			attribute.String("forge.storage.bucket", application.Spec.Storage.Bucket),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	cloudCtx, cancel := context.WithTimeout(ctx, storageReconcileTimeout)
	defer cancel()

	start := time.Now()
	defer func() {
		forgemetrics.StorageReconcileDuration.WithLabelValues(provider).Observe(time.Since(start).Seconds())
	}()

	storageManager, err := newS3StorageManager(
		cloudCtx,
		r.Client,
		application,
		serviceAccountNameFor(application),
		r.OIDCProviderARN,
		r.OIDCProviderURL,
		r.PermissionsBoundaryARN,
		func(ctx context.Context) error {
			application.Status.Storage = &forgev1alpha1.StorageStatus{
				Provider:  forgev1alpha1.ProviderAWSS3,
				Bucket:    application.Spec.Storage.Bucket,
				Created:   true,
				CreatedAt: metav1.Now(),
			}
			return retryStatusUpdate(ctx, r.Client, application)
		},
		r.S3RateLimiter,
		r.IAMRateLimiter,
	)

	if err != nil {
		storagestatus.SetNotReady(application, err)
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, classifyAWSStorageError(err)).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return fmt.Errorf("failed to create S3 storage manager: %w", err)
	}

	result, err := storageManager.ReconcileBucket(cloudCtx)
	if err != nil {
		outcome := classifyAWSStorageError(err)
		if outcome == outcomeNotOwned {
			storagestatus.SetNotOwned(application, err)
		} else {
			storagestatus.SetNotReady(application, err)
		}
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcome).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return fmt.Errorf("failed to reconcile S3 bucket: %w", err)
	}
	if result.RoleARN != "" {
		if err := r.annotateServiceAccountWithIRSA(ctx, application, result.RoleARN); err != nil {
			return err
		}
	}

	// CreatedAt is carried forward from whatever ReconcileBucket already
	// durably recorded, never reset to "now" on a routine reconcile of an
	// already-owned bucket -- otherwise bucketCreationClaimWindow's bound in
	// the s3 package becomes meaningless.
	createdAt := metav1.Now()
	if application.Status.Storage != nil && !application.Status.Storage.CreatedAt.IsZero() {
		createdAt = application.Status.Storage.CreatedAt
	}
	storageStatus := &forgev1alpha1.StorageStatus{
		Provider:       forgev1alpha1.ProviderAWSS3,
		Bucket:         application.Spec.Storage.Bucket,
		Region:         application.Spec.Storage.Region,
		Created:        true,
		CreatedAt:      createdAt,
		SecretName:     application.Spec.Storage.SecretName,
		DeletionPolicy: application.Spec.Storage.DeletionPolicy,
		AWS: &forgev1alpha1.AWSStorageStatus{
			RoleARN: result.RoleARN,
		},
	}

	storagestatus.SetReady(application, storageStatus, "S3 bucket and IRSA role provisioned")

	if err := retryStatusUpdate(ctx, r.Client, application); err != nil {
		return fmt.Errorf("failed to update storage status: %w", err)
	}

	forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeReady).Inc()
	forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(1)

	return nil

}

// reconcileAkamaiStorage provisions the Akamai bucket/access key and returns
// the raw credentials to its caller for use building the storage Secret.
// It deliberately does not persist AccessKey/SecretKey anywhere on
// application.Status: see the AkamaiStorageStatus doc comment for why.
func (r *ApplicationReconciler) reconcileAkamaiStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (result *akamaiobjstr.StorageResult, err error) {

	provider := string(forgev1alpha1.ProviderAkamaiObjectStorage)

	ctx, span := forgemetrics.Tracer().Start(ctx, "reconcileAkamaiStorage",
		trace.WithAttributes(
			attribute.String("forge.storage.provider", provider),
			attribute.String("forge.storage.bucket", application.Spec.Storage.Bucket),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	cloudCtx, cancel := context.WithTimeout(ctx, storageReconcileTimeout)
	defer cancel()

	start := time.Now()
	defer func() {
		forgemetrics.StorageReconcileDuration.WithLabelValues(provider).Observe(time.Since(start).Seconds())
	}()

	storageManager, err := newAkamaiStorageManager(
		cloudCtx,
		r.Client,
		application,
		r.DefaultAkamaiRegion,
		func(ctx context.Context) error {
			application.Status.Storage = &forgev1alpha1.StorageStatus{
				Provider:  forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:    application.Spec.Storage.Bucket,
				Created:   true,
				CreatedAt: metav1.Now(),
			}
			return retryStatusUpdate(ctx, r.Client, application)
		},
		r.AkamaiAccountRateLimiter,
		r.AkamaiObjectRateLimiter,
	)
	if err != nil {
		storagestatus.SetNotReady(application, err)
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, classifyAkamaiStorageError(err)).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return nil, fmt.Errorf("failed to create Akamai storage manager: %w", err)
	}

	result, err = storageManager.ReconcileBucket(cloudCtx)
	if err != nil {
		outcome := classifyAkamaiStorageError(err)
		if outcome == outcomeNotOwned {
			storagestatus.SetNotOwned(application, err)
		} else {
			storagestatus.SetNotReady(application, err)
		}
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcome).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return nil, fmt.Errorf("failed to reconcile Akamai bucket: %w", err)
	}

	// Akamai only returns the secret key once, at creation. On later
	// reconciles, recover it from the storage Secret this controller
	// previously wrote, rather than caching it on the Application (status
	// is far more widely readable than a Secret).
	if result.SecretKey == "" {
		existing := &corev1.Secret{}
		key := types.NamespacedName{Name: naming.StorageSecret(application), Namespace: application.Namespace}
		if err := r.Get(ctx, key, existing); err == nil {
			result.SecretKey = string(existing.Data["secret_key"])
		}
	}

	// See the identical comment in reconcileAWSStorage: CreatedAt must be
	// carried forward, never regenerated, or bucketCreationClaimWindow's
	// bound becomes meaningless.
	createdAt := metav1.Now()
	if application.Status.Storage != nil && !application.Status.Storage.CreatedAt.IsZero() {
		createdAt = application.Status.Storage.CreatedAt
	}
	// Recorded onto Status.Storage.Akamai so cleanup can still resolve the
	// right input token Secret after spec.storage is removed (see
	// storageSpecFromStatus). Guarded defensively in case the defaulting
	// webhook is disabled.
	accessKeySecretRef := ""
	if application.Spec.Storage.Akamai != nil {
		accessKeySecretRef = application.Spec.Storage.Akamai.AccessKeySecretRef
	}

	storageStatus := &forgev1alpha1.StorageStatus{
		Provider:       forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:         application.Spec.Storage.Bucket,
		Region:         application.Spec.Storage.Region,
		Created:        true,
		CreatedAt:      createdAt,
		SecretName:     application.Spec.Storage.SecretName,
		DeletionPolicy: application.Spec.Storage.DeletionPolicy,
		Akamai: &forgev1alpha1.AkamaiStorageStatus{
			Endpoint:           result.Endpoint,
			AccessKeySecretRef: accessKeySecretRef,
		},
	}

	storagestatus.SetReady(application, storageStatus, "Akamai bucket and access key provisioned")

	if err := retryStatusUpdate(ctx, r.Client, application); err != nil {
		return nil, fmt.Errorf("failed to update storage status: %w", err)
	}

	forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeReady).Inc()
	forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(1)

	return result, nil
}
