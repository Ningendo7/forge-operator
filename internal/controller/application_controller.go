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
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	statusmanager "github.com/Ningendo7/forge-operator/internal/controller/status"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
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
) (result ctrl.Result, err error) {

	// Root span for this reconcile -- every other span this operator
	// creates nests under this one because ctx carries it from here on.
	// One trace per reconcile is the whole point: open one in Jaeger and
	// see exactly where the time went, not just that it went somewhere.
	ctx, span := forgemetrics.Tracer().Start(ctx, "Reconcile",
		trace.WithAttributes(
			attribute.String("forge.application.name", req.Name),
			attribute.String("forge.application.namespace", req.Namespace),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Enrich ctx itself with these fields, not just the local logger --
	// every downstream call derives its own logger via logf.FromContext(ctx), and none of
	// them previously had any way to know which Application they belonged
	// to. Without this, every one of those logs is identical across every
	// Application in every namespace -- impossible to tell apart once more
	// than one Application is reconciling concurrently (MaxConcurrentReconciles
	// makes that the normal case, not an edge case).
	logger := logf.FromContext(ctx, "application", req.Name, "namespace", req.Namespace)
	ctx = logf.IntoContext(ctx, logger)
	logger.Info("Reconciling Application")

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
		logger.Error(err, "Error handling finalizer for Application")
		return ctrl.Result{}, err
	}

	if isDeleting {
		// If the application is being deleted, no further processing is needed
		return ctrl.Result{}, nil
	}

	if err := r.ensureDesiredState(ctx, application); err != nil {
		logger.Error(err, "Failed to reconcile desired state")
		if statusErr := r.StatusManager.SetFailed(ctx, application, err); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		forgemetrics.ApplicationReady.WithLabelValues(application.Namespace, application.Name).Set(0)
		return ctrl.Result{}, err
	}

	ready, reason, err := r.StatusManager.EvaluateComputeReadiness(ctx, application)
	if err != nil {
		logger.Error(err, "Failed to evaluate Application readiness")
		if statusErr := r.StatusManager.SetFailed(ctx, application, err); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		forgemetrics.ApplicationReady.WithLabelValues(application.Namespace, application.Name).Set(0)
		return ctrl.Result{}, err
	}

	if !ready {
		logger.Info("Application not yet ready", "reason", reason)
		if err := r.StatusManager.SetReconciling(ctx, application, reason); err != nil {
			return ctrl.Result{}, err
		}
		forgemetrics.ApplicationReady.WithLabelValues(application.Namespace, application.Name).Set(0)
		return ctrl.Result{RequeueAfter: readinessRequeueInterval}, nil
	}

	logger.Info("Successfully reconciled Application")
	if err := r.StatusManager.SetReady(ctx, application, reason); err != nil {
		return ctrl.Result{}, err
	}
	forgemetrics.ApplicationReady.WithLabelValues(application.Namespace, application.Name).Set(1)

	if application.Spec.Storage != nil {
		return ctrl.Result{RequeueAfter: storageResyncInterval}, nil
	}

	return ctrl.Result{}, nil
}

// applicationChangePredicate re-reconciles on a real spec change (generation
// bump) or the one true transition into being marked for deletion, but
// ignores pure status/metadata-only updates otherwise. Without the
// generation check, every status write Reconcile makes to the Application
// (SetReconciling/SetReady/SetFailed) would itself be an update the primary
// watch below sees and re-triggers on, causing the controller to reconcile
// itself in an unbounded loop even once fully settled -- deletionTimestamp
// going from unset to set is included explicitly because that transition
// doesn't bump generation either, and finalizer cleanup depends on seeing
// it.
//
// Deliberately checks the *transition* (old nil, new non-nil), not just
// "new is non-nil" -- an earlier version checked only the latter, which
// matches every subsequent update to an already-deleting object too,
// including the object's own status writes from a failed/stuck cleanup
// attempt. Confirmed live: an Application whose finalizer cleanup keeps
// failing (e.g. bucket ownership verification failing before deletion) has
// its own SetCleanupInProgress/SetFailed writes each independently pass
// this predicate, re-triggering Reconcile via this watch immediately --
// completely bypassing the workqueue's own exponential backoff on the
// error, since a fresh watch-triggered Add() isn't rate-limited the way
// AddRateLimited's error-driven retry is. The result was a sustained,
// non-decaying reconcile storm for the entire time a deletion stayed stuck,
// not just a brief burst -- worse than the Owns()-predicate storms fixed
// elsewhere in this file, since it can happen to any Application whose
// deletion is stuck for any reason. Once only the true transition passes,
// every later reconcile of an already-deleting Application (including ones
// its own failed attempts write) correctly falls through to
// GenerationChangedPredicate, which returns false for a status-only change
// -- leaving the workqueue's own rate-limited retry as the only thing
// driving further attempts, with real backoff. A controller restart while a
// deletion is stuck loses nothing: the informer's initial List always
// delivers a Create event, and Create is unconditionally let through by
// both predicates below regardless of this Update-only logic.
var applicationChangePredicate = predicate.Or(
	predicate.GenerationChangedPredicate{},
	predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetDeletionTimestamp() == nil && e.ObjectNew.GetDeletionTimestamp() != nil
		},
	},
)

// ownedGenerationChangedPredicate filters Owns() watch events for an owned
// resource whose STATUS this operator never reads: only Service qualifies
// here (EvaluateComputeReadiness Get()s it just to confirm it exists, never
// inspects .status). Service has its own spec/status split, so Kubernetes
// only bumps .metadata.generation on a genuine .spec change. Without this,
// every Server-Side Apply this controller makes to it -- even a fully
// idempotent one -- updates .metadata.managedFields[].time, which bumps
// resourceVersion and fires an Update event; an unfiltered Owns() watch
// turns that straight back into a new Reconcile call, which applies the
// same content again, which bumps managedFields again -- an unbounded,
// self-sustaining storm with no external trigger at all. Mirrors
// applicationChangePredicate's own reasoning above, extended to the owned
// resources it was never applied to.
//
// Deliberately NOT used for Deployment/Ingress/HorizontalPodAutoscaler/
// PodDisruptionBudget even though they have the same generation semantics
// -- see ownedStatusOrGenerationChangedPredicate below for why those four
// need a different predicate instead.
var ownedGenerationChangedPredicate = predicate.GenerationChangedPredicate{}

// ownedStatusOrGenerationChangedPredicate is for owned resources whose
// STATUS this operator's own readiness evaluation actually depends on --
// Deployment (.status.readyReplicas et al, via IsDeploymentReady), Ingress
// (.status.loadBalancer, via IsIngressReady), HorizontalPodAutoscaler (via
// IsHPAReady), and PodDisruptionBudget (via IsPDBReady) -- see
// StatusManager.EvaluateComputeReadiness. Using plain
// ownedGenerationChangedPredicate here was tried first and was wrong: a
// status subresource write never bumps .metadata.generation, so it filtered
// out 100% of status updates to these four types -- but every status write
// to any of them comes from a controller other than this one
// (kube-controller-manager/kubelet for Deployment, the ingress controller,
// the HPA controller, the disruption controller), never from this
// operator's own SSA re-apply of their .spec. So status-only updates here
// were never the self-inflicted no-op churn ownedGenerationChangedPredicate
// exists to filter out in the first place -- dropping them broke readiness
// tracking entirely instead: a real, healthy rollout landing in .status was
// never noticed, and Ready got stuck flapping. This reacts to a genuine
// .spec change (generation differs) OR a genuine .status change (content
// differs); a pure metadata/managedFields/resourceVersion-only churn from
// this operator's own repeated SSA apply of unchanged .spec is still
// filtered out, same as ownedGenerationChangedPredicate's own reasoning.
var ownedStatusOrGenerationChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
			return true
		}
		switch oldObj := e.ObjectOld.(type) {
		case *appsv1.Deployment:
			newObj, ok := e.ObjectNew.(*appsv1.Deployment)
			return !ok || !apiequality.Semantic.DeepEqual(oldObj.Status, newObj.Status)
		case *networkingv1.Ingress:
			newObj, ok := e.ObjectNew.(*networkingv1.Ingress)
			return !ok || !apiequality.Semantic.DeepEqual(oldObj.Status, newObj.Status)
		case *autoscalingv2.HorizontalPodAutoscaler:
			newObj, ok := e.ObjectNew.(*autoscalingv2.HorizontalPodAutoscaler)
			return !ok || !apiequality.Semantic.DeepEqual(oldObj.Status, newObj.Status)
		case *policyv1.PodDisruptionBudget:
			newObj, ok := e.ObjectNew.(*policyv1.PodDisruptionBudget)
			return !ok || !apiequality.Semantic.DeepEqual(oldObj.Status, newObj.Status)
		default:
			// An unrecognized type reaching here would be a real bug (this
			// predicate is only ever attached to the Owns() calls below for
			// the four types above) -- fail open rather than silently
			// swallowing an update we don't know how to evaluate.
			return true
		}
	},
}

// ownedContentChangedPredicate is the equivalent protection for owned
// resources with no generation semantics at all (ConfigMap, Secret,
// ServiceAccount are flat data, not spec/status types -- Kubernetes never
// increments their .metadata.generation, so ownedGenerationChangedPredicate
// can't distinguish a real content change from an SSA no-op here). Compares
// only the fields this controller's own desired-state builders ever
// populate (see desiredConfigMap, desiredStorage, desiredServiceAccount +
// the IRSA role-arn annotation in serviceaccount.go) -- an update that
// changes only bookkeeping metadata (managedFields, resourceVersion,
// ownerReferences) is filtered out. Also used on the Secret Watches() below,
// which is the second amplifier for this same storm on the storage Secret
// specifically.
var ownedContentChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		switch oldObj := e.ObjectOld.(type) {
		case *corev1.ConfigMap:
			newObj, ok := e.ObjectNew.(*corev1.ConfigMap)
			return !ok ||
				!apiequality.Semantic.DeepEqual(oldObj.Data, newObj.Data) ||
				!apiequality.Semantic.DeepEqual(oldObj.BinaryData, newObj.BinaryData)
		case *corev1.Secret:
			newObj, ok := e.ObjectNew.(*corev1.Secret)
			return !ok || !apiequality.Semantic.DeepEqual(oldObj.Data, newObj.Data)
		case *corev1.ServiceAccount:
			newObj, ok := e.ObjectNew.(*corev1.ServiceAccount)
			return !ok ||
				!apiequality.Semantic.DeepEqual(oldObj.Annotations, newObj.Annotations) ||
				!apiequality.Semantic.DeepEqual(oldObj.Secrets, newObj.Secrets) ||
				!apiequality.Semantic.DeepEqual(oldObj.ImagePullSecrets, newObj.ImagePullSecrets) ||
				!apiequality.Semantic.DeepEqual(oldObj.AutomountServiceAccountToken, newObj.AutomountServiceAccountToken)
		default:
			// An unrecognized type reaching here would be a real bug (this
			// predicate is only ever attached to the Owns()/Watches() calls
			// below for the three types above) -- fail open rather than
			// silently swallowing an update we don't know how to evaluate.
			return true
		}
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&forgev1alpha1.Application{}, builder.WithPredicates(applicationChangePredicate)).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(ownedStatusOrGenerationChangedPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(ownedGenerationChangedPredicate)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(ownedContentChangedPredicate)).
		Owns(&corev1.Secret{}, builder.WithPredicates(ownedContentChangedPredicate)).
		Owns(&networkingv1.Ingress{}, builder.WithPredicates(ownedStatusOrGenerationChangedPredicate)).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}, builder.WithPredicates(ownedStatusOrGenerationChangedPredicate)).
		Owns(&policyv1.PodDisruptionBudget{}, builder.WithPredicates(ownedStatusOrGenerationChangedPredicate)).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(ownedContentChangedPredicate)).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findApplicationsForSecret),
			builder.WithPredicates(ownedContentChangedPredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Named("application").
		Complete(r)
}
