package status

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

)

func (s *StatusManager) EvaluateComputeReadiness(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (bool, string, error) {

	svc := &corev1.Service{}
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: application.Namespace, Name: application.Name}, svc); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false, fmt.Sprintf("Service %s/%s not found", application.Namespace, application.Name), nil
		}
		return false, fmt.Sprintf("Error fetching Service %s/%s: %v", application.Namespace, application.Name, err), err
	}

	ready, msg, err := s.IsDeploymentReady(ctx, application.Namespace, application.Name)
	if !ready || err != nil {
		return ready, msg, err
	}

	if application.Spec.Ingress != nil {
		ready, msg, err := s.IsIngressReady(ctx, application.Namespace, application.Name)
		if !ready || err != nil {
			return ready, msg, err
		}
	}

	if application.Spec.Autoscaling != nil {
		ready, msg, err := s.IsHPAReady(ctx, application.Namespace, application.Name)
		if !ready || err != nil {
			return ready, msg, err
		}
	}

	return true, "All Compute resources are ready", nil
}
	