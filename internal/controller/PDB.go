package controller

import (

	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

)

func (r *ApplicationReconciler) desiredPDB(
	application *forgev1alpha1.Application,
) *policyv1.PodDisruptionBudget {

	labels := map[string]string{"app": application.Name}

	pdbSpec := policyv1.PodDisruptionBudgetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: labels},
	}

	if application.Spec.PDB != nil {
		if application.Spec.PDB.MinAvailable != nil {
			pdbSpec.MinAvailable = application.Spec.PDB.MinAvailable
		} else if application.Spec.PDB.MaxUnavailable != nil {
			// minAvailable and maxUnavailable are mutually exclusive, so we only set one of them
			pdbSpec.MaxUnavailable = application.Spec.PDB.MaxUnavailable
		}
	}

	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PodDisruptionBudget",
			APIVersion: "policy/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name + "-pdb",
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Spec: pdbSpec,
	}
}

func (r *ApplicationReconciler) reconcilePDB(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	if application.Spec.PDB == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling PodDisruptionBudget")

	desired := r.desiredPDB(application)

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for PodDisruptionBudget: %w", err)
	}

	err := r.Patch(
		ctx, 
		desired, 
		client.Apply, 
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	)
	if err != nil {
		logger.Error(err, "Failed to apply PodDisruptionBudget", "name", desired.Name)
		return fmt.Errorf("failed to apply PodDisruptionBudget: %w", err)
	}

	logger.Info("Successfully reconciled PodDisruptionBudget", "name", desired.Name)
	return nil
}
