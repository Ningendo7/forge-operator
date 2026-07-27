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

	lb := ingress.Status.LoadBalancer.Ingress
	// Controller has NOT reconciled yet → lb == nil
	if lb == nil {
		return false, "Ingress pending controller reconciliation", nil
	}

	// Controller has reconciled but no IP/Hostname assigned yet → lb == []
	if len(lb) == 0 {
		return false, "Ingress reconciled: (pending IP/Hostname from controller)", nil
	}

	// LB ingress exists → cloud ingress controllers
	endpoint := lb[0].IP
	if endpoint == "" {
		endpoint = lb[0].Hostname
	}

	return true, fmt.Sprintf("Ingress assigned endpoint: %s", endpoint), nil
}
