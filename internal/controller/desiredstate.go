package controller

import (
	"context"

	demov1 "example.com/my-controller/api/v1alpha1"
	s3status "example.com/my-controller/internal/controller/s3"
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



}