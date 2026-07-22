package status

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
		return false, fmt.Sprintf("Deployment %s %s not found:", namespace, name), client.IgnoreNotFound(err)
	}

	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false, fmt.Sprintf("Deployment rollout in progress: status generation (%d) lags spec generation %d", deployment.Status.ObservedGeneration, deployment.Generation), nil
	}

	// Check for failed or stalled rollouts
	for _, cond := range deployment.Status.Conditions {

		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			return false, fmt.Sprintf("Deployment rollout issue: %s", cond.Message), nil
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			return false, fmt.Sprintf("Deployment failed creating pods: %s", cond.Message), nil
		}

	}

	desiredReplicas := int32(1)

	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	if deployment.Status.UpdatedReplicas < desiredReplicas {
		msg := fmt.Sprintf("Deployment rollout in progress: %d/%d updated", deployment.Status.UpdatedReplicas, desiredReplicas)
		return false, msg, nil
	}

	if deployment.AvailableReplicas < desiredReplicas {
		msg := fmt.Sprintf("Waiting for pod availability: %d/%d available", deployment.Status.AvailableReplicas, desiredReplicas)
		return false, msg, nil
	}
	
	if deployment.Status.ReadyReplicas < desiredReplicas {

		msg := fmt.Sprintf("Deployment pods are not ready: %d/%d ready", deployment.Status.ReadyReplicas, desiredReplicas)
		return false, msg, nil
	}

	return true, "Deployment pods are fully ready", nil
}