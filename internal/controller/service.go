package controller

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ApplicationReconciler) desiredService(
	application *forgev1alpha1.Application,
) *corev1.Service {

	labels := map[string]string{
		"app": application.Name,
	}

	serviceType := corev1.ServiceTypeClusterIP
	if application.Spec.Service.Type != "" {
		serviceType = application.Spec.Service.Type
	}

	servicePort := int32(80)
	if application.Spec.Service.Port != 0 {
		servicePort = application.Spec.Service.Port
	}

	targetPort := int32(8080)
	if application.Spec.Container.Port != 0 {
		targetPort = application.Spec.Container.Port
	}
	if application.Spec.Service.TargetPort != nil {
		targetPort = *application.Spec.Service.TargetPort
	}

	return &corev1.Service{

		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       servicePort,
				TargetPort: intstr.FromInt(int(targetPort)),
			}},
		},
	}
}

func (r *ApplicationReconciler) reconcileService(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Service")

	desired := r.desiredService(application)

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for Service: %w", err)
	}

	err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("forge-operator"),
	)
	if err != nil {
		logger.Error(err, "failed to apply Service", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply Service: %w", err)
	}

	logger.Info("Successfully reconciled Service", "name", desired.Name)

	return nil
}
