package akamaiobjstr

import (
	"errors"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetStorageReady_SetsConditionAndStatus(t *testing.T) {
	app := newTestApp()
	status := &forgev1alpha1.StorageStatus{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: "demo-bucket"}

	SetStorageReady(app, status, "bucket ready")

	if app.Status.Storage != status {
		t.Fatalf("expected status.Storage to be set")
	}

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %q", cond.Status)
	}
	if cond.Reason != ReasonBucketProvisioned {
		t.Errorf("expected reason %q, got %q", ReasonBucketProvisioned, cond.Reason)
	}
	if cond.Message != "bucket ready" {
		t.Errorf("expected message %q, got %q", "bucket ready", cond.Message)
	}
}

func TestSetStorageNotReady_SetsConditionFalse(t *testing.T) {
	app := newTestApp()

	SetStorageNotReady(app, errors.New("boom"))

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition status False, got %q", cond.Status)
	}
	if cond.Reason != ReasonProvisioningFailed {
		t.Errorf("expected reason %q, got %q", ReasonProvisioningFailed, cond.Reason)
	}
	if cond.Message != "boom" {
		t.Errorf("expected message %q, got %q", "boom", cond.Message)
	}
}

func TestUpdateCondition_AppendsNewConditionType(t *testing.T) {
	app := newTestApp()

	updateCondition(app, metav1.Condition{Type: "TypeA", Status: metav1.ConditionTrue})
	updateCondition(app, metav1.Condition{Type: "TypeB", Status: metav1.ConditionFalse})

	if len(app.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(app.Status.Conditions))
	}
}

func TestUpdateCondition_ReplacesExistingConditionType(t *testing.T) {
	app := newTestApp()

	updateCondition(app, metav1.Condition{Type: StorageReady, Status: metav1.ConditionFalse, Reason: "first"})
	updateCondition(app, metav1.Condition{Type: StorageReady, Status: metav1.ConditionTrue, Reason: "second"})

	if len(app.Status.Conditions) != 1 {
		t.Fatalf("expected condition to be replaced in place, got %d conditions", len(app.Status.Conditions))
	}
	if app.Status.Conditions[0].Reason != "second" {
		t.Errorf("expected condition to be updated to 'second', got %q", app.Status.Conditions[0].Reason)
	}
}

func findCondition(app *forgev1alpha1.Application, condType string) *metav1.Condition {
	for i := range app.Status.Conditions {
		if app.Status.Conditions[i].Type == condType {
			return &app.Status.Conditions[i]
		}
	}
	return nil
}
