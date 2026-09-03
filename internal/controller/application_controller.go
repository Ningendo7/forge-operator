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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	statusmanager "github.com/Ningendo7/forge-operator/internal/controller/status"
)

// readinessRequeueInterval is how soon Reconcile re-checks readiness when not yet ready.
const readinessRequeueInterval = 10 * time.Second

// storageResyncInterval is how often a settled, storage-backed Application re-verifies its cloud bucket still exists.
const storageResyncInterval = 10 * time.Minute

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        events.EventRecorder
	OIDCProviderARN string
	OIDCProviderURL string

	// DefaultAkamaiRegion is used for Akamai/Linode storage when an
	// Application doesn't set spec.storage.region itself. It should match
	// wherever this operator's own deployment's Akamai/Linode infrastructure
	// actually lives (see DEFAULT_AKAMAI_REGION in cmd/main.go) — there's no
	// further built-in fallback, since a value baked into the binary would
	// only ever be correct for one specific deployment.
	DefaultAkamaiRegion string

	StatusManager *statusmanager.StatusManager
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=forge.ningendo7.github.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forge.ningendo7.github.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forge.ningendo7.github.io,resources=applications/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives the cluster state toward the Application's desired state.
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
	// Mark that reconciliation has started
	if err := r.StatusManager.SetReconciling(ctx, application, "Reconciling Application resources"); err != nil {
		return ctrl.Result{}, err
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
		if statusErr := r.StatusManager.SetFailed(ctx, application, err); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	ready, reason, err := r.StatusManager.EvaluateComputeReadiness(ctx, application)
	if err != nil {
		logger.Error(err, "Failed to evaluate Application readiness", "name", req.Name, "namespace", req.Namespace)
		if statusErr := r.StatusManager.SetFailed(ctx, application, err); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	if !ready {
		logger.Info("Application not yet ready", "name", req.Name, "namespace", req.Namespace, "reason", reason)
		if err := r.StatusManager.SetReconciling(ctx, application, reason); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: readinessRequeueInterval}, nil
	}

	logger.Info("Successfully reconciled Application", "name", req.Name, "namespace", req.Namespace)
	if err := r.StatusManager.SetReady(ctx, application, reason); err != nil {
		return ctrl.Result{}, err
	}

	if application.Spec.Storage != nil {
		return ctrl.Result{RequeueAfter: storageResyncInterval}, nil
	}

	return ctrl.Result{}, nil
}

// applicationChangePredicate re-reconciles on a real spec change (generation
// bump) or when the object is marked for deletion, but ignores pure
// status/metadata-only updates. Without this, every status write Reconcile
// makes to the Application (SetReconciling/SetReady/SetFailed) would itself
// be an update the primary watch below sees and re-triggers on, causing the
// controller to reconcile itself in an unbounded loop even once fully
// settled -- deletionTimestamp is included explicitly because it doesn't
// bump generation either, and finalizer cleanup depends on that event.
var applicationChangePredicate = predicate.Or(
	predicate.GenerationChangedPredicate{},
	predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetDeletionTimestamp() != nil
		},
	},
)

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&forgev1alpha1.Application{}, builder.WithPredicates(applicationChangePredicate)).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&corev1.ServiceAccount{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findApplicationsForSecret),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Named("application").
		Complete(r)
}
