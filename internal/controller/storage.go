package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
)

func (r *ApplicationReconciler) reconcileStorage(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	// If storage spec is nil, cleanup any existing storage resources and return
	if application.Spec.Storage == nil {
		return r.reconcileStorageSecret(ctx, application)
	}

	// Provision Backend Cloud Storage Resources
	switch application.Spec.Storage.Provider {
	case "AWS", "aws", "S3", "s3", forgev1alpha1.StorageProviderAWS:
		if err := r.reconcileAWSStorage(ctx, application); err != nil {
			return fmt.Errorf("failed to reconcile AWS storage: %w", err)
		}
	case "Akamai", "akamai", forgev1alpha1.StorageProviderAkamai:
		if err := r.reconcileAkamaiStorage(ctx, application); err != nil {
			return fmt.Errorf("failed to reconcile Akamai storage: %w", err)
		}
	case "MinIO", "minio", "Static":
		
	default:
		err := fmt.Errorf("unsupported storage provider: %s", application.Spec.Storage.Provider)
		s3storage.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return err
	}

	// Reconcile Storage Secret
	if err := r.reconcileStorageSecret(ctx, application); err != nil {
		return fmt.Errorf("failed to reconcile storage secret: %w", err)
	}

	return nil
}

func (r *ApplicationReconciler) reconcileAWSStorage(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	storageManager, err := s3storage.NewManager(ctx, r.Client, application)
	if err != nil {
		s3storage.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return fmt.Errorf("failed to create S3 storage manager: %w", err)
	}

	// Reconcile Bucket and IRSA
	if err := storageManager.ReconcileBucket(ctx); err != nil {
		s3storage.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return fmt.Errorf("failed to reconcile S3 bucket: %w", err)
	}

	// Structured Status metdata
	storageStatus := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.StorageProviderAWS,
		Bucket:   application.Spec.Storage.Bucket,
		Region:   application.Spec.Storage.Region,
		AWS: 	 &forgev1alpha1.AWSStorageStatus{
			RoleARN: fmt.Sprintf("arn:aws:iam::%s:role/app-irsa-%s", "YOUR_AWS_ACCOUNT_ID", application.Name),
		},
	}

	s3storage.SetStorageReady(application, storageStatus, "S3 bucket and IRSA role provisioned")

	if err := r.Status().Update(ctx, application); err != nil {
		return fmt.Errorf("failed to update storage status: %w", err)
	}

	return nil

}