package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

// --- applicationChangePredicate ---

func TestApplicationChangePredicate_ReactsToGenerationChange(t *testing.T) {
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 2}}

	if !applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a real spec change (generation bump) to pass the predicate")
	}
}

func TestApplicationChangePredicate_IgnoresStatusOnlyChange(t *testing.T) {
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	newObj.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}

	if applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a status-only write (same generation) to be filtered out")
	}
}

func TestApplicationChangePredicate_ReactsToDeletionTransition(t *testing.T) {
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	now := metav1.Now()
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now, Finalizers: []string{"f"}}}

	if !applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected the nil -> non-nil DeletionTimestamp transition to pass the predicate")
	}
}

func TestApplicationChangePredicate_IgnoresRepeatedUpdatesWhileAlreadyDeleting(t *testing.T) {
	// This is the actual regression: an earlier version of this predicate
	// checked only "new DeletionTimestamp is non-nil", which matches every
	// subsequent update to an already-deleting object too -- including the
	// object's own status writes from a failed/stuck finalizer cleanup
	// attempt (e.g. bucket ownership verification refusing to delete).
	// Confirmed live against a real EKS cluster: this produced a sustained,
	// non-decaying reconcile storm for as long as the deletion stayed
	// stuck, since each failed attempt's own status write immediately
	// re-triggered another reconcile via this predicate, completely
	// bypassing the workqueue's own exponential backoff on the error.
	now := metav1.Now()
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now, Finalizers: []string{"f"}}}
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now, Finalizers: []string{"f"}}}
	newObj.Status.Conditions = []metav1.Condition{{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "ReconcileFailed"}}

	if applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a repeated status-only update on an already-deleting Application to be filtered out")
	}
}

func TestApplicationChangePredicate_CreateAndDeleteAlwaysPass(t *testing.T) {
	obj := &forgev1alpha1.Application{}
	if !applicationChangePredicate.Create(event.CreateEvent{Object: obj}) {
		t.Fatalf("expected Create events to always pass (e.g. informer resync after a controller restart)")
	}
	if !applicationChangePredicate.Delete(event.DeleteEvent{Object: obj}) {
		t.Fatalf("expected Delete events to always pass")
	}
}
