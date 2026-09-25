package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/akamaiobjstr"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
)

// finalizerCleanupTimeout bounds each storage cleanup attempt during
// finalization. Deliberately longer than storage.go's storageReconcileTimeout:
// provisioning takes less time than cleanup.
// Exists for the same reason storageReconcileTimeout does.
const finalizerCleanupTimeout = 5 * time.Minute

func (r *ApplicationReconciler) handleFinalizer(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (bool, error) {

	logger := logf.FromContext(ctx)

	// Check if the Object is being deleted
	if !application.DeletionTimestamp.IsZero() {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(application, ApplicationFinalizer) {
			logger.Info("Application is being deleted, running cleanup finalizer")

			// Perform cleanup of associated resources
			if err := r.finalizeApplication(ctx, application); err != nil {
				return true, fmt.Errorf("failed to finalize application: %w", err)
			}

			// Remove the finalizer to allow deletion to proceed
			controllerutil.RemoveFinalizer(application, ApplicationFinalizer)
			if err := r.Update(ctx, application); err != nil {
				return true, fmt.Errorf("failed to remove finalizer: %w", err)
			}

			logger.Info("Cleanup finalizer completed, finalizer removed")
			forgemetrics.ApplicationReady.DeleteLabelValues(application.Namespace, application.Name)
		}
		return true, nil // Object is being deleted, no further processing needed
	}

	// Object is active, ensure the finalizer is attached
	if !controllerutil.ContainsFinalizer(application, ApplicationFinalizer) {
		logger.Info("Adding finalizer to Application")
		controllerutil.AddFinalizer(application, ApplicationFinalizer)
		if err := r.Update(ctx, application); err != nil {
			return false, fmt.Errorf("failed to add finalizer: %w", err)
		}

		logger.Info("Finalizer added to Application")
	}

	return false, nil // Object is not being deleted, continue processing
}

// storageSpecFromStatus reconstructs enough of StorageSpec to run cleanup
// against a bucket described only by Status.Storage -- used when
// spec.storage has already been removed (or an Application is deleted after
// that removal) but the cloud resource it described hasn't been cleaned up
// yet. SecretName/DeletionPolicy are recorded on StorageStatus for exactly
// this: once spec.storage is gone, they're the only remaining record of
// which credentials to use and whether the bucket should actually be
// deleted.
//
// Akamai.AccessKeySecretRef and AWS.CredentialsSecretRef must be carried
// forward too: akamaiobjstr.NewManager resolves the input token Secret via
// naming.AkamaiTokenSecret(app), which falls back to a default name whenever
// Spec.Storage.Akamai is nil, breaking cleanup for any Akamai Application
// using a customized accessKeySecretRef -- s3storage.NewManager has the same
// problem for AWS static credentials if Spec.Storage.AWS is nil.
func storageSpecFromStatus(status *forgev1alpha1.StorageStatus) *forgev1alpha1.StorageSpec {
	spec := &forgev1alpha1.StorageSpec{
		Provider:       status.Provider,
		Bucket:         status.Bucket,
		Region:         status.Region,
		SecretName:     status.SecretName,
		DeletionPolicy: status.DeletionPolicy,
	}
	if status.Akamai != nil && status.Akamai.AccessKeySecretRef != "" {
		spec.Akamai = &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: status.Akamai.AccessKeySecretRef}
	}
	if status.AWS != nil && status.AWS.CredentialsSecretRef != "" {
		spec.AWS = &forgev1alpha1.AWSStorageSpec{CredentialsSecretRef: status.AWS.CredentialsSecretRef}
	}
	return spec
}

func (r *ApplicationReconciler) finalizeApplication(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (err error) {

	// spec.storage may already be gone -- either removed from an
	// otherwise-still-live Application, or the Application was deleted after
	// that removal before cleanup ran. Status.Storage is the only remaining
	// record of what to clean up in that case.
	//
	// Kept in a local variable, never written onto application.Spec.Storage
	// itself: every r.Status().Update(ctx, application) call below re-syncs
	// application's non-status fields from the server (a status-subresource
	// response decodes back into the same object pointer), which would
	// revert a synthesized Spec.Storage back to nil mid-function. cleanupApp
	// below is a throwaway snapshot so NewManager sees a stable value
	// regardless of how many status updates application goes through after.
	storage := application.Spec.Storage
	if storage == nil && application.Status.Storage != nil {
		storage = storageSpecFromStatus(application.Status.Storage)
	}

	if storage == nil {
		return nil
	}

	// Drop this Application's StorageReady gauge series once finalization
	// genuinely succeeds (storage cleaned up, retained, or never configured)
	// -- but not if it fails, since the finalizer stays attached and the
	// Application isn't actually gone yet, so the gauge is still meaningful.
	provider := storage.Provider
	defer func() {
		if err == nil {
			forgemetrics.StorageReady.DeleteLabelValues(application.Namespace, application.Name, string(provider))
		}
	}()

	if storage.DeletionPolicy == forgev1alpha1.DeletionPolicyRetain {
		r.cleanupRetainedStorageCredentials(ctx, application, storage)
		r.retainStorage(ctx, application, storage.Bucket)
		return nil
	}

	cleanupApp := application.DeepCopy()
	cleanupApp.Spec.Storage = storage

	switch provider {
	case forgev1alpha1.ProviderAWSS3:
		storagestatus.SetCleanupInProgress(application)
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))

		cloudCtx, cancel := context.WithTimeout(ctx, finalizerCleanupTimeout)
		defer cancel()

		providerStr := string(forgev1alpha1.ProviderAWSS3)
		start := time.Now()

		storageManager, mgrErr := s3storage.NewManager(
			cloudCtx,
			r.Client,
			cleanupApp,
			serviceAccountNameFor(application),
			r.OIDCProviderARN,
			r.OIDCProviderURL,
			r.PermissionsBoundaryARN,
			nil, // cleanup never creates a bucket, so recordCreated is never invoked
			r.S3RateLimiter,
			r.IAMRateLimiter,
		)

		if mgrErr != nil {
			forgemetrics.FinalizerCleanupDuration.WithLabelValues(providerStr).Observe(time.Since(start).Seconds())
			forgemetrics.FinalizerCleanupTotal.WithLabelValues(providerStr, classifyAWSStorageError(mgrErr)).Inc()
			return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to create storage manager for cleanup: %w", mgrErr))
		}

		irsaErr, cleanupErr := storageManager.CleanupBucket(cloudCtx)
		if cleanupErr != nil {
			forgemetrics.FinalizerCleanupDuration.WithLabelValues(providerStr).Observe(time.Since(start).Seconds())
			forgemetrics.FinalizerCleanupTotal.WithLabelValues(providerStr, classifyAWSStorageError(cleanupErr)).Inc()
			return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to delete S3 bucket during cleanup: %w", cleanupErr))
		}

		forgemetrics.FinalizerCleanupDuration.WithLabelValues(providerStr).Observe(time.Since(start).Seconds())
		forgemetrics.FinalizerCleanupTotal.WithLabelValues(providerStr, outcomeSuccess).Inc()
		if irsaErr != nil {
			// Logged, not just evented -- the Application (and its Events)
			// are about to be deleted, so the Event alone wouldn't outlive
			// the object it's attached to.
			logf.FromContext(ctx).Error(irsaErr, "Failed to clean up IRSA role/policy after successful bucket deletion", "application", application.Name)
			if r.Recorder != nil {
				r.Recorder.Eventf(application, nil, corev1.EventTypeWarning, "IRSACleanupFailed", "Cleanup",
					"Bucket was deleted, but its IAM role/policy could not be cleaned up: %v", irsaErr)
			}
		}
	case forgev1alpha1.ProviderAkamaiObjectStorage:
		storagestatus.SetCleanupInProgress(application)
		logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))

		cloudCtx, cancel := context.WithTimeout(ctx, finalizerCleanupTimeout)
		defer cancel()

		providerStr := string(forgev1alpha1.ProviderAkamaiObjectStorage)
		start := time.Now()

		storageManager, mgrErr := akamaiobjstr.NewManager(
			cloudCtx,
			r.Client,
			cleanupApp,
			r.DefaultAkamaiRegion,
			nil, // cleanup never creates a bucket, so recordCreated is never invoked
			r.AkamaiAccountRateLimiter,
			r.AkamaiObjectRateLimiter,
		)

		if mgrErr != nil {
			forgemetrics.FinalizerCleanupDuration.WithLabelValues(providerStr).Observe(time.Since(start).Seconds())
			forgemetrics.FinalizerCleanupTotal.WithLabelValues(providerStr, classifyAkamaiStorageError(mgrErr)).Inc()
			return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to create Akamai storage manager for cleanup: %w", mgrErr))
		}

		accessKeyErr, cleanupErr := storageManager.DeleteBucket(cloudCtx)
		if cleanupErr != nil {
			forgemetrics.FinalizerCleanupDuration.WithLabelValues(providerStr).Observe(time.Since(start).Seconds())
			forgemetrics.FinalizerCleanupTotal.WithLabelValues(providerStr, classifyAkamaiStorageError(cleanupErr)).Inc()
			return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to delete Akamai bucket during cleanup: %w", cleanupErr))
		}

		forgemetrics.FinalizerCleanupDuration.WithLabelValues(providerStr).Observe(time.Since(start).Seconds())
		forgemetrics.FinalizerCleanupTotal.WithLabelValues(providerStr, outcomeSuccess).Inc()
		if accessKeyErr != nil {
			logf.FromContext(ctx).Error(accessKeyErr, "Failed to clean up Akamai Object Storage access key after successful bucket deletion", "application", application.Name)
			if r.Recorder != nil {
				r.Recorder.Eventf(application, nil, corev1.EventTypeWarning, "AccessKeyCleanupFailed", "Cleanup",
					"Bucket was deleted, but its Akamai Object Storage access key could not be cleaned up: %v", accessKeyErr)
			}
		}
	}
	return nil
}

// cleanupRetainedStorageCredentials deletes this Application's IAM
// role/access key even though deletionPolicy is Retain -- the bucket itself
// is left alone (see retainStorage below), but a later Application that
// adopts the retained bucket mints its own fresh credential regardless
// (the old secret was only ever exposed once, in this Application's own
// output Secret, which is deleted along with it), so the old credential
// would otherwise sit untracked and still-valid in the cloud account
// indefinitely.
//
// Best-effort and never fails the Application's own deletion, the same as
// retainStorage. Failures are surfaced as a Warning Event, the same
// visibility non-Retain credential-cleanup failures get.
func (r *ApplicationReconciler) cleanupRetainedStorageCredentials(
	ctx context.Context,
	application *forgev1alpha1.Application,
	storage *forgev1alpha1.StorageSpec,
) {
	logger := logf.FromContext(ctx)
	cleanupApp := application.DeepCopy()
	cleanupApp.Spec.Storage = storage

	cloudCtx, cancel := context.WithTimeout(ctx, finalizerCleanupTimeout)
	defer cancel()

	switch storage.Provider {
	case forgev1alpha1.ProviderAWSS3:
		storageManager, mgrErr := s3storage.NewManager(
			cloudCtx,
			r.Client,
			cleanupApp,
			serviceAccountNameFor(application),
			r.OIDCProviderARN,
			r.OIDCProviderURL,
			r.PermissionsBoundaryARN,
			nil, // cleanup never creates a bucket, so recordCreated is never invoked
			r.S3RateLimiter,
			r.IAMRateLimiter,
		)
		if mgrErr != nil {
			logger.Error(mgrErr, "Failed to build S3 manager to clean up retained bucket's IRSA role")
			return
		}
		if err := storageManager.CleanupCredentialsOnly(cloudCtx); err != nil {
			logger.Error(err, "Failed to clean up IRSA role for retained bucket", "bucket", storage.Bucket)
			if r.Recorder != nil {
				r.Recorder.Eventf(application, nil, corev1.EventTypeWarning, "IRSACleanupFailed", "Cleanup",
					"Bucket was retained, but its IAM role/policy could not be cleaned up: %v", err)
			}
		}
	case forgev1alpha1.ProviderAkamaiObjectStorage:
		storageManager, mgrErr := akamaiobjstr.NewManager(
			cloudCtx,
			r.Client,
			cleanupApp,
			r.DefaultAkamaiRegion,
			nil, // cleanup never creates a bucket, so recordCreated is never invoked
			r.AkamaiAccountRateLimiter,
			r.AkamaiObjectRateLimiter,
		)
		if mgrErr != nil {
			logger.Error(mgrErr, "Failed to build Akamai manager to clean up retained bucket's access key")
			return
		}
		if err := storageManager.CleanupCredentialsOnly(cloudCtx); err != nil {
			logger.Error(err, "Failed to clean up access key for retained bucket", "bucket", storage.Bucket)
			if r.Recorder != nil {
				r.Recorder.Eventf(application, nil, corev1.EventTypeWarning, "AccessKeyCleanupFailed", "Cleanup",
					"Bucket was retained, but its Akamai Object Storage access key could not be cleaned up: %v", err)
			}
		}
	}
}

// retainStorage skips *bucket* deletion when spec.storage.deletionPolicy is
// Retain: the bucket (and its ownership tag/marker) is left exactly as-is,
// only the Kubernetes Application object and its finalizer are removed --
// Retain protects the data, not the credential that reaches it (see
// cleanupRetainedStorageCredentials above). Emits an Event so this is
// visible and auditable. bucket is passed in explicitly rather than read
// from application.Spec.Storage since the caller may be cleaning up a
// bucket described only by Status.Storage. Never fails.
func (r *ApplicationReconciler) retainStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
	bucket string,
) {
	storagestatus.SetRetained(application, bucket)
	logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))

	if r.Recorder != nil {
		r.Recorder.Eventf(application, nil, corev1.EventTypeNormal, "StorageRetained", "Cleanup",
			"deletionPolicy is Retain: bucket %q was NOT deleted and remains in your cloud account", bucket)
	}

	logf.FromContext(ctx).Info("Storage retained per deletionPolicy, skipping cloud cleanup", "bucket", bucket)
}

// failStorageCleanup records the StorageReady condition as cleanup-failed
// (best-effort) and returns the original error unchanged.
func (r *ApplicationReconciler) failStorageCleanup(
	ctx context.Context,
	application *forgev1alpha1.Application,
	err error,
) error {
	storagestatus.SetCleanupFailed(application, err)
	logStorageStatusUpdateError(ctx, retryStatusUpdate(ctx, r.Client, application))
	return err
}
