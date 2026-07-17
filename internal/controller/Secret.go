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

func (r *ApplicationReconciler) desiredSecret(application *forgev1alpha1.Application) *corev1.Secret {
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Type:       secretType,
		StringData: secretData,
	}
}

func (r *ApplicationReconciler) desiredStorage(application *forgev1alpha1.Application) *corev1.Secret {
	if application.Spec.Storage == nil {
		return nil
	}

	name := application.Name + "-storage"
	if application.Spec.Storage.SecretName != "" {
		name = application.Spec.Storage.SecretName
	}

	return &corev1.Secret{
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

func (r *ApplicationReconciler) getSecret(ctx context.Context, key client.ObjectKey) (*corev1.Secret, error) {
	var existing corev1.Secret
	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *ApplicationReconciler) reconcileSecret(ctx context.Context, application *forgev1alpha1.Application) error {
	if application.Spec.Secret == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Secret")

	desired := r.desiredSecret(application)
	existing, err := r.getSecret(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace})
	if apierrors.IsNotFound(err) {
		logger.Info("Creating Secret", "name", desired.Name)
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
	existing.StringData = desired.StringData
	existing.Type = desired.Type

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch Secret", "name", existing.Name)
		return err
	}

	logger.Info("Updated Secret", "name", existing.Name)
	return nil
}

func (r *ApplicationReconciler) reconcileStorage(ctx context.Context, application *forgev1alpha1.Application) error {
	if application.Spec.Storage == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Storage Secret")

	desired := r.desiredStorage(application)
	if desired == nil {
		return nil
	}

	existing, err := r.getSecret(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace})
	if apierrors.IsNotFound(err) {
		logger.Info("Creating Storage Secret", "name", desired.Name)
		if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	} else if err != nil {
		return err
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.StringData = desired.StringData
	existing.Type = desired.Type

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch Storage Secret", "name", existing.Name)
		return err
	}

	logger.Info("Updated Storage Secret", "name", existing.Name)
	return nil
}

