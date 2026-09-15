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
// bump), the one true transition into being marked for deletion, or a
// change to the adopt-bucket annotation's value, but ignores pure
// status/other-metadata-only updates otherwise. Without the generation
// check, every status write Reconcile makes to the Application
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
// every predicate below regardless of this Update-only logic.
//
// The adopt-bucket branch closes a real usability gap, not a storm risk:
// naming.AdoptBucketAnnotation is metadata, not spec, so setting or
// clearing it -- the documented way to opt into reclaiming a bucket left
// behind by a Application that's genuinely gone -- never bumped generation
// either, and was silently swallowed by this predicate exactly like any
// other annotation-only change. In practice this is most often reached by a
// user reacting to a live BucketNotOwned failure by annotating the
// already-existing Application, not by setting it at creation (Create
// events always pass regardless of this Update-only logic, so a brand-new
// Application created with the annotation already set was never affected).
// That failing Application is typically sitting on the workqueue's own
// exponential backoff from the ownership failure itself; without this
// branch, the annotation is only ever picked up whenever that backoff next
// happens to fire, with no visible signal that anything happened in the
// meantime -- easily misread as "adopt-bucket doesn't work" rather than
// "adopt-bucket hasn't been retried yet." Scoped to only this one
// annotation key, not annotations generally, to avoid reopening the same
// class of self-triggering risk the rest of this predicate exists to
// prevent.
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
// STATUS this operator's own readiness evaluation actually depends on --
// Deployment (.status.readyReplicas et al, via IsDeploymentReady), Ingress
// (.status.loadBalancer, via IsIngressReady), HorizontalPodAutoscaler (via
// IsHPAReady), and PodDisruptionBudget (via IsPDBReady) -- see
// StatusManager.EvaluateComputeReadiness. Using a plain
// predicate.GenerationChangedPredicate here was tried first and was wrong: a
// status subresource write never bumps .metadata.generation, so it filtered
// out 100% of status updates to these four types -- but every status write
// to any of them comes from a controller other than this one
// (kube-controller-manager/kubelet for Deployment, the ingress controller,
// the HPA controller, the disruption controller), never from this
// operator's own SSA re-apply of their .spec. So status-only updates here
// were never the self-inflicted no-op churn a plain generation-changed
// predicate exists to filter out in the first place -- dropping them broke
// readiness tracking entirely instead: a real, healthy rollout landing in
// .status was never noticed, and Ready got stuck flapping. This reacts to a
// genuine .spec change (generation differs) OR a genuine .status change
// (content differs); a pure metadata/managedFields/resourceVersion-only
// churn from this operator's own repeated SSA apply of unchanged .spec is
// still filtered out, same reasoning as a plain generation-changed
// predicate.
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
// ServiceAccount, and Service are flat data or otherwise-untracked spec
// types -- Kubernetes never increments their .metadata.generation, so
// ownedGenerationChangedPredicate can't distinguish a real content change
// from an SSA no-op here -- confirmed live for Service specifically: a
// direct kubectl edit to spec.selector, corrupting Service routing, was
// silently never corrected, because ownedGenerationChangedPredicate was
// wired to it before this fix despite Service having exactly the same
// missing-generation problem as the three types already routed here).
// Compares only the fields this controller's own desired-state builders
// ever populate (see desiredConfigMap, desiredStorage, desiredServiceAccount,
// desiredService + the IRSA role-arn annotation in serviceaccount.go) -- an
// update that changes only bookkeeping metadata (managedFields,
// resourceVersion, ownerReferences) is filtered out. Also used on the
// Secret Watches() below, which is the second amplifier for this same
// storm on the storage Secret specifically.
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
