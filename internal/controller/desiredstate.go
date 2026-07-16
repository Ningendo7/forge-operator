package controller

import (
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

	if err := r.reconcileHPA(ctx, application); err != nil {
		return err
	}

	return nil
}