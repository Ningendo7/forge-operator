package controller

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ApplicationReconciler) desiredHPA(
	application *forgev1alpha1.Application,
) *autoscalingv2.HorizontalPodAutoscaler {
	if application.Spec.Autoscaling == nil {
		return nil
	}

	autoscaling := application.Spec.Autoscaling

	minReplicas := int32(1)
	if autoscaling.MinReplicas > 0 {
		minReplicas = autoscaling.MinReplicas
	}

	maxReplicas := int32(3)
	if autoscaling.MaxReplicas > 0 {
		maxReplicas = autoscaling.MaxReplicas
	}
	if maxReplicas < minReplicas {
		maxReplicas = minReplicas
	}

	avgUtilization := int32(80)
	if autoscaling.CPUUtilization != nil {
		avgUtilization = *autoscaling.CPUUtilization
	}
	if avgUtilization < 1 {
		avgUtilization = 1
	}
	if avgUtilization > 100 {
		avgUtilization = 100
	}

	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name + "-hpa",
			Namespace: application.Namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       application.Name + "-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &avgUtilization,
						},
					},
				},
			},
		},
	}
}

func (r *ApplicationReconciler) getHPA(
	ctx context.Context,
	key client.ObjectKey,
) (*autoscalingv2.HorizontalPodAutoscaler, error) {

	var existing autoscalingv2.HorizontalPodAutoscaler

	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}

	return &existing, nil
}

func (r *ApplicationReconciler) reconcileHPA(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling HPA")

	desired := r.desiredHPA(application)

	if desired == nil {
		return nil
	}

	existing, err := r.getHPA(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	})

	if apierrors.IsNotFound(err) {
		logger.Info("Creating HPA", "name", desired.Name)

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

	existing.Spec.ScaleTargetRef = desired.Spec.ScaleTargetRef
	existing.Spec.MinReplicas = desired.Spec.MinReplicas
	existing.Spec.MaxReplicas = desired.Spec.MaxReplicas
	existing.Spec.Metrics = desired.Spec.Metrics

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "Failed to patch HPA", "name", existing.Name)
		return err
	}

	logger.Info("Updated HPA", "name", existing.Name)

	return nil
}
