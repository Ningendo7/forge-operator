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

func (r *ApplicationReconciler) desiredSecret(
	application *forgev1alpha1.Application,
) *corev1.Secret {

	labels := map[string]string{"app": application.Name}
	name := application.Name + "-secret"
	secretType := corev1.SecretTypeOpaque
	secretData := map[string]string{}

	if application.Spec.Secret != nil {
		if application.Spec.Secret.Name != "" {
			name = application.Spec.Secret.Name
		}
		if application.Spec.Secret.Type != "" {
			secretType = application.Spec.Secret.Type
		}
		secretData = application.Spec.Secret.StringData
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Type:       secretType,
		StringData: secretData,
	}
}

func (r *ApplicationReconciler) desiredStorage(
	application *forgev1alpha1.Application,
) *corev1.Secret {

	if application.Spec.Storage == nil {
		return nil
	}

	name := application.Name + "-storage"
	if application.Spec.Storage.SecretName != "" {
		name = application.Spec.Storage.SecretName
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    map[string]string{"app": application.Name},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"provider": application.Spec.Storage.Provider,
			"bucket":   application.Spec.Storage.Bucket,
			"region":   application.Spec.Storage.Region,
			"endpoint": application.Spec.Storage.Endpoint,
		},
	}
}

func (r *ApplicationReconciler) reconcileSecret(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	if application.Spec.Secret == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Secret")

	desired := r.desiredSecret(application)

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for Secret: %w", err)
	}

	err := r.Patch(
		ctx, 
		desired, 
		client.Apply, 
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply Secret", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply Secret: %w", err)
	}

	logger.Info("Successfully reconciled Secret", "name", desired.Name)
	return nil
}

func (r *ApplicationReconciler) reconcileStorageSecret(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	if application.Spec.Storage == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Storage Secret")

	desired := r.desiredStorage(application)
	if desired == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for Storage Secret: %w", err)
	}

	err := r.Patch(
		ctx, 
		desired, 
		client.Apply, 
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply Storage Secret", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply Storage Secret: %w", err)
	}

	logger.Info("Successfully reconciled Storage Secret", "name", desired.Name)
	return nil
}