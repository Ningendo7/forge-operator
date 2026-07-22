package status

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsHPAReady verifies whether the HPA is active
func (s *StatusManager) IsHPAReady(
	ctx context.Context,
	namespace,
	name string,
) (bool, string, error) {

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, hpa)

	if err != nil {
		return false, fmt.Sprintf("HPA %s %s not found:", namespace, name), client.IgnoreNotFound(err)
	}

	if hpa.Status.ObservedGeneration < hpa.Generation {
		return false, fmt.Sprintf("HPA rollout in progress: status generation (%d) lags spec generation %d", hpa.Status.ObservedGeneration, hpa.Generation), nil
	}
	
	for _, cond := range hpa.Status.Conditions {
		if cond.Type == autoscalingv2.AbleToScale && cond.Status == "False" {

			return false, fmt.Sprintf("HPA scaling issue: %s", cond.Message), nil
		}
	}

	return true, fmt.Sprintf("HPA active(current replicas: %d)", hpa.Status.CurrentReplicas), nil
}