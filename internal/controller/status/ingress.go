package status

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

)

// IsIngressReady verifies whether the ingress has recieved an IP or Hostname assignment
func (s *StatusManager) IsIngressReady(
	ctx context.Context,
	namespace,
	name string,
) (bool, string, error) {

	ingress := &networkingv1.Ingress{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, ingress)
	if err != nil {
		return false, fmt.Sprintf("Ingress %s %s not found:", namespace, name), client.IgnoreNotFound(err)
	}

	if ingress.Status.ObservedGeneration < ingress.Generation {
		return false, fmt.Sprintf("Ingress rollout in progress: status generation (%d) lags spec generation %d", ingress.Status.ObservedGeneration, ingress.Generation), nil
	}

	if len(ingress.Status.LoadBalancer.Ingress) == 0 {
		return false, "Ingress pending IP/Hostname from controller", nil
	}

	endpoint := ingress.Status.LoadBalancer.Ingress[0].IP
	if endpoint == "" {
		endpoint = ingress.Status.LoadBalancer.Ingress[0].Hostname
	}

	return true, fmt.Sprintf("Ingress assigned endpoint: %s", endpoint), nil
}