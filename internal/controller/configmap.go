package controller

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ApplicationReconciler) desiredConfigMap(
	application *forgev1alpha1.Application,
) *corev1.ConfigMap {
	name := configMapNameFor(application)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels: map[string]string{
				"app": application.Name,
			},
		},
		Data: map[string]string{
			"app-name": application.Name,
			"image":    application.Spec.Image,
		},
	}
}

func (r *ApplicationReconciler) getConfigMap(
	ctx context.Context,
	key client.ObjectKey,
) (*corev1.ConfigMap, error) {
	var existing corev1.ConfigMap
	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *ApplicationReconciler) reconcileConfigMap(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {
	logger := logf.FromContext(ctx)
	logger.Info("Reconciling ConfigMap")

	desired := r.desiredConfigMap(application)

	existing, err := r.getConfigMap(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	})

	if apierrors.IsNotFound(err) {
		logger.Info("Creating ConfigMap", "name", desired.Name)
		if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	} else if err != nil {
		return err
	}

	if !metav1.IsControlledBy(existing, application) {
		if err := controllerutil.SetControllerReference(application, existing, r.Scheme); err != nil {
			return err
		}
	}

	patch := client.MergeFrom(existing.DeepCopy())

	existing.Labels = desired.Labels
	existing.Data = desired.Data

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch ConfigMap", "name", existing.Name)
		return err
	}

	logger.Info("Updated ConfigMap", "name", existing.Name)
	return nil
}
