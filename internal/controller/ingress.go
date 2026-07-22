package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ApplicationReconciler) desiredIngress(
	application *forgev1alpha1.Application,
) *networkingv1.Ingress {

	labels := map[string]string{"app": application.Name}
	ingressSpec := application.Spec.Ingress

	rule := networkingv1.IngressRule{
		Host: ingressSpec.Host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{
					Path:     ingressSpec.Path,
					PathType: ingressSpec.PathType,
					Backend: networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: application.Name,
							Port: networkingv1.ServiceBackendPort{Number: 80},
						},
					},
				}},
			},
		},
	}

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name,
			Namespace: application.Namespace,
			Labels:    labels,
			Annotations: ingressSpec.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressSpec.ClassName,
			Rules:            []networkingv1.IngressRule{rule},
		},
	}
}

func (r *ApplicationReconciler) reconcileIngress(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)

	// Handle Toggling Ingress: If the Ingress is disabled, we should delete it if it exists.
	if application.Spec.Ingress == nil {
		ing := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      application.Name,
				Namespace: application.Namespace,
			},
		}
		if err := r.Delete(ctx, ing); client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete disabled Ingress", "name", ing.Name)
			return fmt.Errorf("failed to delete disabled Ingress: %w", err)
		}
		
		logger.Info("Successfully deleted disabled Ingress", "name", ing.Name)
		return nil
	}

	logger.Info("Reconciling Ingress")

	desired := r.desiredIngress(application)

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for Ingress: %w", err)
	}

	err := r.Patch(
		ctx, 
		desired, 
		client.Apply, 
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply Ingress", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply Ingress: %w", err)
	}

	logger.Info("Successfully reconciled Ingress", "name", desired.Name)
	return nil
}