package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

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

func (r *ApplicationReconciler) finalizeApplication(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	if application.Spec.Storage != nil {
		switch application.Spec.Storage.Provider {
		case forgev1alpha1.ProviderAWSS3:
			storagestatus.SetCleanupInProgress(application)
			logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))

			storageManager, err := s3storage.NewManager(
				ctx,
				r.Client,
				application,
				serviceAccountNameFor(application),
				r.OIDCProviderARN,
				r.OIDCProviderURL,
			)
			if err != nil {
				return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to create storage manager for cleanup: %w", err))
			}
			if err := storageManager.CleanupBucket(ctx); err != nil {
				return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to delete S3 bucket during cleanup: %w", err))
			}
		case forgev1alpha1.ProviderAkamaiObjectStorage:
			storagestatus.SetCleanupInProgress(application)
			logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))

			storageManager, err := akamaiobjstr.NewManager(
				ctx,
				r.Client,
				application,
				r.DefaultAkamaiRegion,
			)
			if err != nil {
				return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to create Akamai storage manager for cleanup: %w", err))
			}
			accessKeyErr, err := storageManager.DeleteBucket(ctx)
			if err != nil {
				return r.failStorageCleanup(ctx, application, fmt.Errorf("failed to delete Akamai bucket during cleanup: %w", err))
			}
			if accessKeyErr != nil && r.Recorder != nil {
				r.Recorder.Eventf(application, nil, corev1.EventTypeWarning, "AccessKeyCleanupFailed", "Cleanup",
					"Bucket was deleted, but its Akamai Object Storage access key could not be cleaned up: %v", accessKeyErr)
			}
		}
	}
	return nil
}

// failStorageCleanup records the StorageReady condition as cleanup-failed
// (best-effort) and returns the original error unchanged.
func (r *ApplicationReconciler) failStorageCleanup(
	ctx context.Context,
	application *forgev1alpha1.Application,
	err error,
) error {
	storagestatus.SetCleanupFailed(application, err)
	logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))
	return err
}
