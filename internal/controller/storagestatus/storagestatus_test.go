package storagestatus

import (
	"errors"
	"strings"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestApp() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
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

func TestSetReady_SetsConditionAndStatus(t *testing.T) {
	app := newTestApp()
	status := &forgev1alpha1.StorageStatus{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}

	SetReady(app, status, "bucket ready")

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
	if cond.Reason != ReasonBucketConfigured {
		t.Errorf("expected reason %q, got %q", ReasonBucketConfigured, cond.Reason)
	}
	if cond.Message != "bucket ready" {
		t.Errorf("expected message %q, got %q", "bucket ready", cond.Message)
	}
}

func TestSetReady_NilAppIsNoOp(t *testing.T) {
	SetReady(nil, &forgev1alpha1.StorageStatus{}, "should not panic")
}

func TestSetNotReady_SetsConditionFalse(t *testing.T) {
	app := newTestApp()

	SetNotReady(app, errors.New("boom"))

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition status False, got %q", cond.Status)
	}
	if cond.Reason != ReasonBucketConfigurationFailed {
		t.Errorf("expected reason %q, got %q", ReasonBucketConfigurationFailed, cond.Reason)
	}
	if !strings.Contains(cond.Message, "boom") {
		t.Errorf("expected message to contain %q, got %q", "boom", cond.Message)
	}
}

func TestSetNotReady_NilErrorUsesDefaultMessage(t *testing.T) {
	app := newTestApp()

	SetNotReady(app, nil)

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if cond.Message != "Storage configuration failed" {
		t.Errorf("expected default message, got %q", cond.Message)
	}
}

func TestSetNotReady_TruncatesLongErrorMessage(t *testing.T) {
	app := newTestApp()
	longMsg := strings.Repeat("x", MaxErrorMessageLength+50)

	SetNotReady(app, errors.New(longMsg))

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if !strings.HasSuffix(cond.Message, "...") {
		t.Errorf("expected truncated message to end with '...', got %q", cond.Message)
	}
	if len(cond.Message) > MaxErrorMessageLength+len("Storage configuration failed: ") {
		t.Errorf("expected message to be truncated, got length %d", len(cond.Message))
	}
}

func TestSetCleanupInProgress_SetsConditionFalse(t *testing.T) {
	app := newTestApp()

	SetCleanupInProgress(app)

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition status False, got %q", cond.Status)
	}
	if cond.Reason != ReasonBucketCleanup {
		t.Errorf("expected reason %q, got %q", ReasonBucketCleanup, cond.Reason)
	}
}

func TestSetCleanupFailed_SetsConditionFalse(t *testing.T) {
	app := newTestApp()

	SetCleanupFailed(app, errors.New("cleanup boom"))

	cond := findCondition(app, StorageReady)
	if cond == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if cond.Reason != ReasonBucketCleanupFailed {
		t.Errorf("expected reason %q, got %q", ReasonBucketCleanupFailed, cond.Reason)
	}
	if !strings.Contains(cond.Message, "cleanup boom") {
		t.Errorf("expected message to contain %q, got %q", "cleanup boom", cond.Message)
	}
}

func TestSetCleanupFailed_NilAppIsNoOp(t *testing.T) {
	SetCleanupFailed(nil, errors.New("boom"))
}

func TestSetCleanupInProgress_NilAppIsNoOp(t *testing.T) {
	SetCleanupInProgress(nil)
}

func TestSetNotReady_NilAppIsNoOp(t *testing.T) {
	SetNotReady(nil, errors.New("boom"))
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		maxLength int
		expected  string
	}{
		{name: "shorter than max is unchanged", message: "short", maxLength: 10, expected: "short"},
		{name: "longer than max is truncated with ellipsis", message: "1234567890", maxLength: 5, expected: "12..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateMessage(tt.message, tt.maxLength); got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestSetReadyThenNotReady_UpdatesSameConditionInPlace(t *testing.T) {
	app := newTestApp()

	SetReady(app, &forgev1alpha1.StorageStatus{}, "ready")
	SetNotReady(app, errors.New("now failing"))

	if len(app.Status.Conditions) != 1 {
		t.Fatalf("expected exactly 1 StorageReady condition, got %d", len(app.Status.Conditions))
	}
	cond := findCondition(app, StorageReady)
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected condition to flip to False, got %q", cond.Status)
	}
}
