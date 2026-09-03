package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

func shouldCreateServiceAccount(
	application *forgev1alpha1.Application,
) bool {
	if application.Spec.ServiceAccount == nil {
		return true
	}
	if application.Spec.ServiceAccount.Create != nil {
		return *application.Spec.ServiceAccount.Create
	}
	// Only default to creating one when neither field is set.
	return application.Spec.ServiceAccount.Name == ""
}

func serviceAccountNameFor(
	application *forgev1alpha1.Application,
) string {

	if application.Spec.ServiceAccount != nil &&
		application.Spec.ServiceAccount.Name != "" {
		return application.Spec.ServiceAccount.Name
	}
	return application.Name + "-sa"
}

// podServiceAccountName resolves the ServiceAccountName for the desired pod
// spec, or "" when there's genuinely no ServiceAccount to reference. This is
// deliberately independent of shouldCreateServiceAccount: whether the
// operator owns/creates the ServiceAccount and whether the pod should
// reference one are two different questions -- a user-supplied
// (Create: false) ServiceAccount still needs to be wired into the pod spec,
// it just isn't one this operator creates or force-owns. The only case with
// nothing to reference is Create explicitly false with Name unset (rejected
// at admission by the CRD's CEL rule for new/updated objects, but still
// handled defensively here for objects that predate that rule).
func podServiceAccountName(application *forgev1alpha1.Application) string {
	sa := application.Spec.ServiceAccount
	if sa != nil && sa.Create != nil && !*sa.Create && sa.Name == "" {
		return ""
	}
	return serviceAccountNameFor(application)
}

func (r *ApplicationReconciler) desiredServiceAccount(
	application *forgev1alpha1.Application,
) *corev1.ServiceAccount {

	labels := map[string]string{appLabelKey: application.Name}
	name := serviceAccountNameFor(application)

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
	}
}

func (r *ApplicationReconciler) reconcileServiceAccount(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	// Respect user-managed ServiceAccounts
	if !shouldCreateServiceAccount(application) {
		return nil
	}

	desired := r.desiredServiceAccount(application)

	if err := controllerutil.SetControllerReference(
		application,
		desired,
		r.Scheme,
	); err != nil {
		return fmt.Errorf("failed to set controller reference for ServiceAccount: %w", err)
	}

	if err := r.Patch(
		ctx,
		desired,
		client.Apply, //nolint:staticcheck // SSA patch via client.Apply is the standard controller-runtime pattern
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to apply ServiceAccount: %w", err)
	}

	return nil
}

func (r *ApplicationReconciler) annotateServiceAccountWithIRSA(
	ctx context.Context,
	application *forgev1alpha1.Application,
	roleArn string,
) error {

	// A user-managed ServiceAccount (Create: false) must not be force-owned
	// or mutated just because IRSA needs an annotation on *some*
	// ServiceAccount -- the Role ARN is already surfaced on
	// Application.Status.Storage.AWS.RoleARN for them to wire in themselves.
	if !shouldCreateServiceAccount(application) {
		return nil
	}

	desired := r.desiredServiceAccount(application)

	if err := controllerutil.SetControllerReference(
		application,
		desired,
		r.Scheme,
	); err != nil {
		return fmt.Errorf("failed to set controller reference for ServiceAccount: %w", err)
	}

	if desired.Annotations == nil {
		desired.Annotations = make(map[string]string)
	}
	desired.Annotations["eks.amazonaws.com/role-arn"] = roleArn

	if err := r.Patch(
		ctx,
		desired,
		client.Apply, //nolint:staticcheck // SSA patch via client.Apply is the standard controller-runtime pattern
		client.FieldOwner("forge-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to apply ServiceAccount with IRSA annotation: %w", err)
	}

	return nil
}
