/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

const applicationFinalizer = "forge.ningendo7.github.io/finalizer"

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=forge.ningendo7.github.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forge.ningendo7.github.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forge.ningendo7.github.io,resources=applications/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Application object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Application", "name", req.Name, "namespace", req.Namespace)

	application := &forgev1alpha1.Application{}
	if err := r.Get(ctx, req.NamespacedName, application); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := r.ensureFinalizer(ctx, application); err != nil {
		return ctrl.Result{}, err
	}

	if application.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if err := r.setCondition(ctx, application, "Progressing", metav1.ConditionTrue, "Reconciling", "Reconciling the desired application resources"); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureDesiredState(ctx, application); err != nil {
		_ = r.setCondition(ctx, application, "Ready", metav1.ConditionFalse, "ReconcileError", err.Error())
		_ = r.setCondition(ctx, application, "Degraded", metav1.ConditionTrue, "ReconcileError", err.Error())
		return ctrl.Result{}, err
	}

	_ = r.setCondition(ctx, application, "Ready", metav1.ConditionTrue, "Reconciled", "The desired resources are reconciled")
	_ = r.setCondition(ctx, application, "Progressing", metav1.ConditionFalse, "Reconciled", "Reconciliation completed")
	_ = r.setCondition(ctx, application, "Degraded", metav1.ConditionFalse, "Reconciled", "Reconciliation completed")

	return ctrl.Result{}, nil
}

func (r *ApplicationReconciler) ensureFinalizer(ctx context.Context, application *forgev1alpha1.Application) error {
	if application.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(application, applicationFinalizer) {
			controllerutil.RemoveFinalizer(application, applicationFinalizer)
			return r.Update(ctx, application)
		}
		return nil
	}

	if !controllerutil.ContainsFinalizer(application, applicationFinalizer) {
		controllerutil.AddFinalizer(application, applicationFinalizer)
		return r.Update(ctx, application)
	}

	return nil
}

func (r *ApplicationReconciler) setCondition(ctx context.Context, application *forgev1alpha1.Application, conditionType string, status metav1.ConditionStatus, reason, message string) error {
	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: application.Generation,
		LastTransitionTime: metav1.Now(),
	})
	return r.Status().Update(ctx, application)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&forgev1alpha1.Application{}).
		Named("application").
		Complete(r)
}
