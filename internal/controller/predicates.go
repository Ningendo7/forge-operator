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
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

// applicationChangePredicate re-reconciles on a real spec change (generation
// bump), the transition into being marked for deletion, or a change to the
// adopt-bucket annotation, but ignores pure status/metadata-only updates.
//
// Without the generation check, every status write Reconcile makes
// (SetReconciling/SetReady/SetFailed) would itself re-trigger via this same
// watch -- an unbounded self-reconcile loop. Worse, an Application stuck
// retrying a failing finalizer would have its own failure-status writes
// re-trigger Reconcile immediately each time, bypassing the workqueue's
// exponential backoff entirely (a watch-triggered Add() isn't rate-limited
// the way the error-driven retry is) -- a sustained storm for as long as
// the deletion stayed stuck. Checking the deletionTimestamp *transition*
// (nil -> non-nil), not just "new is non-nil", is what fixes that: every
// later update to an already-deleting Application, including its own status
// writes, correctly falls through to GenerationChangedPredicate and is
// dropped.
//
// The adopt-bucket branch exists because naming.AdoptBucketAnnotation is
// metadata, not spec, so it doesn't bump generation either -- without this,
// a user reacting to a live BucketNotOwned failure by annotating the
// Application only sees it picked up whenever the workqueue's existing
// backoff next happens to fire, with no visible signal in between. Scoped
// to this one annotation key specifically, not annotations generally, to
// avoid reopening the same storm risk.
var applicationChangePredicate = predicate.Or(
	predicate.GenerationChangedPredicate{},
	predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetDeletionTimestamp() == nil && e.ObjectNew.GetDeletionTimestamp() != nil
		},
	},
	predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetAnnotations()[naming.AdoptBucketAnnotation] != e.ObjectNew.GetAnnotations()[naming.AdoptBucketAnnotation]
		},
	},
)

// ownedStatusOrGenerationChangedPredicate is for owned resources whose
// STATUS this operator's own readiness evaluation depends on: Deployment,
// Ingress, HorizontalPodAutoscaler, and PodDisruptionBudget (see
// StatusManager.EvaluateComputeReadiness). A plain generation-changed
// predicate is wrong here: status writes to these four always come from a
// *different* controller (kubelet, the ingress/HPA/disruption controllers),
// never bump generation, and never come from this operator's own SSA
// re-apply -- so filtering them out isn't avoiding self-inflicted churn,
// it's dropping the only signal that a real rollout landed, leaving
// readiness stuck. This reacts to a genuine .spec change (generation
// differs) OR a genuine .status change (content differs); a pure
// metadata/managedFields/resourceVersion-only churn is still filtered out.
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

// ownedContentChangedPredicate is for owned resources with no generation
// semantics at all -- ConfigMap, Secret, ServiceAccount, and Service never
// bump .metadata.generation, so a generation-only predicate can't tell a
// real content change from an SSA no-op (or, for Service specifically,
// notice a direct edit at all, since nothing ever bumps generation to
// trigger it). Compares only the fields this controller's own desired-state
// builders populate; a bookkeeping-only change (managedFields,
// resourceVersion, ownerReferences) is filtered out. Also used on the
// Secret Watches() below.
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
		case *corev1.Service:
			newObj, ok := e.ObjectNew.(*corev1.Service)
			return !ok ||
				oldObj.Spec.Type != newObj.Spec.Type ||
				!apiequality.Semantic.DeepEqual(oldObj.Spec.Selector, newObj.Spec.Selector) ||
				!apiequality.Semantic.DeepEqual(oldObj.Spec.Ports, newObj.Spec.Ports)
		default:
			// An unrecognized type reaching here would be a real bug (this
			// predicate is only ever attached to the Owns()/Watches() calls
			// below for the types above) -- fail open rather than silently
			// swallowing an update we don't know how to evaluate.
			return true
		}
	},
}
