package controller

import (

	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

)

func (r *ApplicationReconciler) desiredPDB(
	application *forgev1alpha1.Application,
) *policyv1.PodDisruptionBudget {

	labels := map[string]string{"app": application.Name}
	name := application.Name

	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
		},
	}

	if application.Spec.PDB != nil {
		if application.Spec.PDB.MinAvailable != nil {
			pdb.Spec.MinAvailable = application.Spec.PDB.MinAvailable
		}
		if application.Spec.PDB.MaxUnavailable != nil {
			pdb.Spec.MaxUnavailable = application.Spec.PDB.MaxUnavailable
		}
	}

	return pdb
}

func (r *ApplicationReconciler) getPDB(
	ctx context.Context, 
	key client.ObjectKey,
) (*policyv1.PodDisruptionBudget, error) {

	var existing policyv1.PodDisruptionBudget

	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}

	return &existing, nil
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

	existing, err := r.getPDB(ctx, client.ObjectKey{
		Name: 		desired.Name, 
		Namespace: 	desired.Namespace,
})
	if apierrors.IsNotFound(err) {
		logger.Info("Creating PodDisruptionBudget", "name", desired.Name)
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
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch PodDisruptionBudget", "name", existing.Name)
		return err
	}

	logger.Info("Updated PodDisruptionBudget", "name", existing.Name)
	return nil
}
