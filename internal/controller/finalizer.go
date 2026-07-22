package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ApplicationFinalizer is the unique string key attached to metadata.finalizers of Application resources to ensure cleanup of associated resources before deletion.
	ApplicationFinalizer = "forge.ningendo7.github.io/finalizer"
)

func (r *ApplicationReconciler) handleFinalizer(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (bool, error) {

	logger := logf.FromContext(ctx)

	// Check if the Object is being deleted
	if !application.ObjectMeta.DeletionTimestamp.IsZero() {
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
		case "AWS", "aws", "S3", "s3", forgev1alpha1.StorageProviderS3:
			storageManager, err := s3storage.NewManager(ctx, r.Client, application)
			if err != nil {
				return fmt.Errorf("failed to create storage manager for cleanup: %w", err)
			}
			if err := storageManager.DeleteBucket(ctx); err != nil {
				return fmt.Errorf("failed to delete S3 bucket during cleanup: %w", err)
			}
		}
	}

	return nil
}