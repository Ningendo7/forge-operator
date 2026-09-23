package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
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
	// Checking only "new DeletionTimestamp is non-nil" would match every
	// subsequent update to an already-deleting object too, including its
	// own status writes from a stuck finalizer cleanup attempt -- a
	// sustained reconcile storm that bypasses the workqueue's backoff.
	now := metav1.Now()
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now, Finalizers: []string{"f"}}}
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &now, Finalizers: []string{"f"}}}
	newObj.Status.Conditions = []metav1.Condition{{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "ReconcileFailed"}}

	if applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a repeated status-only update on an already-deleting Application to be filtered out")
	}
}

func TestApplicationChangePredicate_ReactsToAdoptBucketAnnotationChange(t *testing.T) {
	// The annotation is metadata, not spec, so it never bumps generation.
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{
		Generation:  1,
		Annotations: map[string]string{naming.AdoptBucketAnnotation: "true"},
	}}

	if !applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected adding the adopt-bucket annotation to pass the predicate")
	}

	// Clearing it must also pass, symmetrically.
	if !applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: newObj, ObjectNew: oldObj}) {
		t.Fatalf("expected removing the adopt-bucket annotation to pass the predicate")
	}
}

func TestApplicationChangePredicate_IgnoresUnrelatedAnnotationChange(t *testing.T) {
	// Scoped to only the adopt-bucket annotation, not annotations
	// generally -- an unrelated annotation change must still be filtered
	// out, same as before this fix.
	oldObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	newObj := &forgev1alpha1.Application{ObjectMeta: metav1.ObjectMeta{
		Generation:  1,
		Annotations: map[string]string{"some-other-key": "value"},
	}}

	if applicationChangePredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected an unrelated annotation change to still be filtered out")
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

// --- ownedContentChangedPredicate ---

func TestOwnedContentChangedPredicate_ReactsToServiceSelectorChange(t *testing.T) {
	// Service never gets .metadata.generation bumped by Kubernetes, unlike
	// most resources -- a direct edit to spec.selector must still be caught.
	oldObj := &corev1.Service{Spec: corev1.ServiceSpec{Selector: map[string]string{appLabelKey: "demo"}}}
	newObj := &corev1.Service{Spec: corev1.ServiceSpec{Selector: map[string]string{appLabelKey: "wrong-selector"}}}

	if !ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a Service selector change to pass the predicate")
	}
}

func TestOwnedContentChangedPredicate_ReactsToServicePortsChange(t *testing.T) {
	oldObj := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: servicePortName, Port: 80}}}}
	newObj := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: servicePortName, Port: 8080}}}}

	if !ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a Service ports change to pass the predicate")
	}
}

func TestOwnedContentChangedPredicate_ReactsToServiceTypeChange(t *testing.T) {
	oldObj := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}}
	newObj := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}}

	if !ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a Service type change to pass the predicate")
	}
}

func TestOwnedContentChangedPredicate_IgnoresServiceMetadataOnlyChange(t *testing.T) {
	spec := corev1.ServiceSpec{Selector: map[string]string{appLabelKey: "demo"}, Type: corev1.ServiceTypeClusterIP}
	oldObj := &corev1.Service{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1"}, Spec: spec}
	newObj := &corev1.Service{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2"}, Spec: spec}

	if ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a resourceVersion-only Service change (e.g. this operator's own repeated SSA apply) to be filtered out")
	}
}

func TestOwnedContentChangedPredicate_ReactsToConfigMapDataChange(t *testing.T) {
	oldObj := &corev1.ConfigMap{Data: map[string]string{"greeting": "howdy"}}
	newObj := &corev1.ConfigMap{Data: map[string]string{"greeting": "goodbye"}}

	if !ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a ConfigMap data change to pass the predicate")
	}
}

func TestOwnedContentChangedPredicate_ReactsToSecretDataChange(t *testing.T) {
	oldObj := &corev1.Secret{Data: map[string][]byte{"apikey": []byte("old")}}
	newObj := &corev1.Secret{Data: map[string][]byte{"apikey": []byte("new")}}

	if !ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a Secret data change to pass the predicate")
	}
}

func TestOwnedContentChangedPredicate_ReactsToServiceAccountAnnotationChange(t *testing.T) {
	// The IRSA role-arn annotation specifically -- see serviceaccount.go.
	oldObj := &corev1.ServiceAccount{}
	newObj := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/demo"},
	}}

	if !ownedContentChangedPredicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatalf("expected a ServiceAccount annotation change to pass the predicate")
	}
}
