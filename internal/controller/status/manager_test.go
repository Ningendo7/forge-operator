package status

import (
	"context"
	"errors"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestStatusManager(app *forgev1alpha1.Application) *StatusManager {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	return NewStatusManager(fakeClient)
}

func TestSetReconciling_SetsProgressingTrueAndReadyFalse(t *testing.T) {
	app := newTestApplication()
	s := newTestStatusManager(app)

	if err := s.SetReconciling(context.Background(), app, "starting reconcile"); err != nil {
		t.Fatalf("SetReconciling returned error: %v", err)
	}

	progressing := findCondition(app, TypeProgressing)
	if progressing == nil || progressing.Status != metav1.ConditionTrue {
		t.Fatalf("expected Progressing=True, got %#v", progressing)
	}
	if progressing.Message != "starting reconcile" {
		t.Errorf("expected message %q, got %q", "starting reconcile", progressing.Message)
	}

	ready := findCondition(app, TypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False, got %#v", ready)
	}
	if ready.Reason != ReasonReconciling {
		t.Errorf("expected reason %q, got %q", ReasonReconciling, ready.Reason)
	}
}

func TestSetReady_SetsReadyTrueAndClearsProgressingAndDegraded(t *testing.T) {
	app := newTestApplication()
	s := newTestStatusManager(app)

	if err := s.SetReady(context.Background(), app, "all good"); err != nil {
		t.Fatalf("SetReady returned error: %v", err)
	}

	ready := findCondition(app, TypeReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %#v", ready)
	}
	if ready.Message != "all good" {
		t.Errorf("expected message %q, got %q", "all good", ready.Message)
	}

	progressing := findCondition(app, TypeProgressing)
	if progressing == nil || progressing.Status != metav1.ConditionFalse {
		t.Fatalf("expected Progressing=False, got %#v", progressing)
	}

	degraded := findCondition(app, TypeDegraded)
	if degraded == nil || degraded.Status != metav1.ConditionFalse {
		t.Fatalf("expected Degraded=False, got %#v", degraded)
	}
}

func TestSetFailed_SetsDegradedTrueAndReadyFalse(t *testing.T) {
	app := newTestApplication()
	s := newTestStatusManager(app)

	if err := s.SetFailed(context.Background(), app, errors.New("boom")); err != nil {
		t.Fatalf("SetFailed returned error: %v", err)
	}

	degraded := findCondition(app, TypeDegraded)
	if degraded == nil || degraded.Status != metav1.ConditionTrue {
		t.Fatalf("expected Degraded=True, got %#v", degraded)
	}
	if degraded.Message != "boom" {
		t.Errorf("expected degraded message %q, got %q", "boom", degraded.Message)
	}

	ready := findCondition(app, TypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False, got %#v", ready)
	}
	if ready.Reason != ReasonFailed {
		t.Errorf("expected reason %q, got %q", ReasonFailed, ready.Reason)
	}
}

func TestUpdateStatus_PersistsConditions(t *testing.T) {
	app := newTestApplication()
	s := newTestStatusManager(app)

	meta := metav1.Condition{
		Type:    TypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonAvailable,
		Message: "test condition",
	}
	app.Status.Conditions = append(app.Status.Conditions, meta)

	if err := s.UpdateStatus(context.Background(), app); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
}

func TestUpdateStatus_ReturnsErrorWhenClientFails(t *testing.T) {
	app := newTestApplication()
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	s := NewStatusManager(&failingStatusClient{Client: fakeClient})

	if err := s.UpdateStatus(context.Background(), app); err == nil {
		t.Fatalf("expected error from UpdateStatus, got nil")
	}
}
