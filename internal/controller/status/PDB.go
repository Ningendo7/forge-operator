package status

import (
	"context"
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsPDBReady verifies whether the PDB is active and allows disruptions
func (s *StatusManager) IsPDBReady(
	ctx context.Context,
	namespace,
	name string,
) (bool, string, error) {

	pdb := &policyv1.PodDisruptionBudget{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pdb)
	if err != nil {
		return false, "", client.IgnoreNotFound(err)
	}

	if pdb.Status.DisruptionsAllowed == 0 && pdb.Status.CurrentHealthy < pdb.Status.DesiredHealthy {
		return false, fmt.Sprintf("PDB unhealthy: %d/%d healthy pods", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy), nil
	}

	return true, fmt.Sprintf("PDB active and healthy"), nil
	
}