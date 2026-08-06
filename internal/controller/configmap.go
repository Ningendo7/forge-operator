package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func configResourceNameFor(application *forgev1alpha1.Application) string {
	if application.Spec.ConfigMap != nil && application.Spec.ConfigMap.Name != "" {
		return application.Spec.ConfigMap.Name
	}
	return application.Name + "-config"
}

func (r *ApplicationReconciler) desiredConfigMap(
	application *forgev1alpha1.Application,
) *corev1.ConfigMap {

	labels := map[string]string{"app": application.Name}
	data := map[string]string{
		"app-name": application.Name,
		"image":    application.Spec.Image,
	}

	if application.Spec.ConfigMap != nil && len(application.Spec.ConfigMap.Data) > 0 {
		data = application.Spec.ConfigMap.Data
	}

	return &corev1.ConfigMap{

		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configResourceNameFor(application),
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Data: data,
	}
}

func (r *ApplicationReconciler) reconcileConfigMap(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)

	// Handle Toggling ConfigMap: If the ConfigMap is disabled, we should delete it if it exists
	if application.Spec.ConfigMap == nil || len(application.Spec.ConfigMap.Data) == 0 {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configResourceNameFor(application),
				Namespace: application.Namespace,
			},
		}
		if err := r.Delete(ctx, cm); client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete disabled ConfigMap", "name", cm.Name)
			return fmt.Errorf("failed to delete disabled ConfigMap: %w", err)
		}

		logger.Info("Successfully deleted disabled ConfigMap", "name", cm.Name)
		return nil
	}

	logger.Info("Reconciling ConfigMap")

	desired := r.desiredConfigMap(application)

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for ConfigMap: %w", err)
	}

	err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply ConfigMap", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply ConfigMap: %w", err)
	}

	logger.Info("Successfully reconciled ConfigMap", "name", desired.Name)
	return nil
}
