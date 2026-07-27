package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
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
	case forgev1alpha1.ProviderAWSS3:
		if err := r.reconcileAWSStorage(ctx, application); err != nil {
			return fmt.Errorf("failed to reconcile AWS storage: %w", err)
		}
	case forgev1alpha1.ProviderAkamaiObjectStorage:
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

	// Initialize S3 Storage Manager with OIDC info for IRSA role creation

	storageManager, err := s3storage.NewManager(
		ctx,
		r.Client,
		application,
		serviceAccountNameFor(application),
		r.OIDCProviderARN,
		r.OIDCProviderURL,
	)

	if err != nil {
		s3storage.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return fmt.Errorf("failed to create S3 storage manager: %w", err)
	}

	// Reconcile Bucket and IRSA
	result, err := storageManager.ReconcileBucket(ctx)
	if err != nil {
		s3storage.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return fmt.Errorf("failed to reconcile S3 bucket: %w", err)
	}
	if result.RoleARN != "" {
		if err := r.annotateServiceAccountWithIRSA(ctx, application, result.RoleARN); err != nil {
			return err
		}
	}

	// Structured Status metdata
	storageStatus := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   application.Spec.Storage.Bucket,
		Region:   application.Spec.Storage.Region,
		AWS: &forgev1alpha1.AWSStorageStatus{
			RoleARN: result.RoleARN,
		},
	}

	s3storage.SetStorageReady(application, storageStatus, "S3 bucket and IRSA role provisioned")

	if err := r.Status().Update(ctx, application); err != nil {
		return fmt.Errorf("failed to update storage status: %w", err)
	}

	return nil

}

func (r *ApplicationReconciler) reconcileAkamaiStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	// Initialize Akamai Storage Manager
	storageManager, err := akamaiobjstr.NewManager(
		ctx,
		r.Client,
		application,
	)
	if err != nil {
		akamaiobjstr.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return fmt.Errorf("failed to create Akamai storage manager: %w", err)
	}

	// Reconcile Bucket and Access Key
	result, err := storageManager.ReconcileBucket(ctx)
	if err != nil {
		akamaiobjstr.SetStorageNotReady(application, err)
		_ = r.Status().Update(ctx, application)
		return fmt.Errorf("failed to reconcile Akamai bucket: %w", err)
	}

	akamaiStatus := &forgev1alpha1.AkamaiStorageStatus{
		AccessKey: result.AccessKey,
		Endpoint:  result.Endpoint,
	}
	// Preserve existing secret key because akamai only returns it once
	if result.SecretKey != "" {
		akamaiStatus.SecretKey = result.SecretKey
	} else if application.Status.Storage != nil && application.Status.Storage.Akamai != nil {
		akamaiStatus.SecretKey = application.Status.Storage.Akamai.SecretKey
	}

	storageStatus := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   application.Spec.Storage.Bucket,
		Region:   application.Spec.Storage.Region,
		Akamai:   akamaiStatus,
	}

	akamaiobjstr.SetStorageReady(application, storageStatus, "Akamai bucket and access key provisioned")

	if err := r.Status().Update(ctx, application); err != nil {
		return fmt.Errorf("failed to update storage status: %w", err)
	}

	return nil
}
