package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

func shouldCreateServiceAccount(
	application *forgev1alpha1.Application,
) bool {
	if application.Spec.ServiceAccount == nil {
		return true
	}
	if application.Spec.ServiceAccount.Create == nil {
		return true
	}
	return *application.Spec.ServiceAccount.Create
}

func serviceAccountNameFor(
	application *forgev1alpha1.Application,
) string {

	if application.Spec.ServiceAccount != nil &&
		application.Spec.ServiceAccount.Name != "" {
		return application.Spec.ServiceAccount.Name
	}
	return application.Name + "-sa"
}

func (r *ApplicationReconciler) desiredServiceAccount(
	application *forgev1alpha1.Application,
) *corev1.ServiceAccount {

	labels := map[string]string{"app": application.Name}
	name := serviceAccountNameFor(application)

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
	}
}

func (r *ApplicationReconciler) reconcileServiceAccount(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	// Respect user-managed ServiceAccounts
	if !shouldCreateServiceAccount(application) {
		return nil
	}

	desired := r.desiredServiceAccount(application)

	if err := controllerutil.SetControllerReference(
		application, 
		desired, 
		r.Scheme,
	); err != nil {
		return fmt.Errorf("failed to set controller reference for ServiceAccount: %w", err)
	}

	if err := r.Patch(
		ctx, 
		desired, 
		client.Apply, 
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to apply ServiceAccount: %w", err)
	}

	return nil
}