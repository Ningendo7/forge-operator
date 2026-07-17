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
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       servicePort,
				TargetPort: intstr.FromInt(int(targetPort)),
			}},
		},
	}
}

func (r *ApplicationReconciler) getService(
	ctx context.Context,
	key client.ObjectKey,
) (*corev1.Service, error) {

	var existing corev1.Service

	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}

	return &existing, nil
}

func (r *ApplicationReconciler) reconcileService(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Service")

	desired := r.desiredService(application)

	existing, err := r.getService(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	})

	if apierrors.IsNotFound(err) {
		logger.Info("Creating Service", "name", desired.Name)
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
	existing.Spec.Type = desired.Spec.Type
	existing.Spec.Selector = desired.Spec.Selector
	existing.Spec.Ports = desired.Spec.Ports

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch Service", "name", existing.Name)
		return err
	}

	logger.Info("Updated Service", "name", existing.Name)

	return nil
}
