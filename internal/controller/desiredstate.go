package controller

	"fmt"
	"github.com/Ningendo7/forge-operator/internal/controller/s3"
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

func (r *ApplicationReconciler) ensureDesiredState(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	if err := r.reconcileService(ctx, application); err != nil {
		return err
	}

	if err := r.reconcileDeployment(ctx, application); err != nil {
		return err
	}

	if err := r.reconcileConfigMap(ctx, application); err != nil {
		return err
	}

	if err := r.reconcileSecret(ctx, application); err != nil {
		return err
	}

	if err := r.reconcileStorage(ctx, application); err != nil {
		return err
	}

	if err := r.reconcileIngress(ctx, application); err != nil {
		return err
	}

	if err := r.reconcilePDB(ctx, application); err != nil {
		return err
	}

	if err := r.reconcileHPA(ctx, application); err != nil {
		return err
	}

	// Storage reconciliation
	if application.Spec.Storage != nil {

		storageManager, err := s3storage.NewManager(ctx, r.Client, application)
		if err != nil {
			return fmt.Errorf("failed to create S3 storage manager: %w", err)
		}

		if err := storageManager.ReconcileBucket(ctx); err != nil {
			return fmt.Errorf("failed to reconcile S3 bucket: %w", err)
		}
	}

	return nil
}