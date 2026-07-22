package status

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

)

func (s *StatusManager) IsDeploymentReady(
	ctx context.Context,
	namespace,
	name string,
) (bool, string, error) {

	deployment := &appsv1.Deployment{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, deployment)

	if err != nil {
		return false, "", client.IgnoreNotFound(err)
	}

	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	if deployment.Status.ReadyReplicas < desiredReplicas {

		msg := fmt.Sprintf("Deployment pods are not ready: %d/%d ready", deployment.Status.ReadyReplicas, desiredReplicas)
		return false, msg, nil
	}

	return true, "Deployment pods are fully ready", nil
}