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
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

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
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	isDeleting, err := r.handleFinalizer(ctx, application)
	if err != nil {
		logger.Error(err, "Error handling finalizer for Application", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	if isDeleting {
		// If the application is being deleted, no further processing is needed
		return ctrl.Result{}, nil
	}

	if err := r.ensureDesiredState(ctx, application); err != nil {
		logger.Error(err, "Failed to reconcile desired state", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, application); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&forgev1alpha1.Application{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&autoscalingv1.HorizontalPodAutoscaler{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findApplicationsForSecret),
		).
		Named("application").
		Complete(r)
}
