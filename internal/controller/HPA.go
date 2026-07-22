package controller

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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
		TypeMeta: metav1.TypeMeta{
			Kind:       "HorizontalPodAutoscaler",
			APIVersion: "autoscaling/v2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name + "-hpa",
			Namespace: application.Namespace,
			Labels: map[string]string{
				"app": application.Name,
			},
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

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for HPA: %w", err)
	}

	err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply HPA", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply HPA: %w", err)
	}

	logger.Info("Successfully reconciled HPA", "name", desired.Name)

	return nil
}
