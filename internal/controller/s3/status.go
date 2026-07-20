package s3storage

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

const (
	StorageReady = "StorageReady"

	ReasonBucketConfigured = "BucketConfigured"
	ReasonBucketConfigurationFailed = "BucketConfigurationFailed"
	ReasonBucketCleanup = "BucketCleanup"
	ReasonBucketCleanupFailed = "BucketCleanupFailed"
)

func SetStorageReady(app *forgev1alpha1.Application, 
	message string,
) {
	metav1.SetStatusCondition(
		&app.Status.Conditions, 
		metav1.Condition{
			Type:    StorageReady,
			Status:  metav1.ConditionTrue,
			Reason:  ReasonBucketConfigured,
			Message: message,
			ObservedGeneration: app.Generation,
		},
	)
}

func SetStorageNotReady(
	app *forgev1alpha1.Application,
	err error,
) {
	metav1.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:    StorageReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonBucketConfigurationFailed,
			Message: fmt.Sprintf("Storage configuration failed: %v", err),
			ObservedGeneration: app.Generation,
		},
	)
}

func SetStorageCleanupInProgress(
	app *forgev1alpha1.Application,
) {
	metav1.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:    StorageReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonBucketCleanup,
			Message: "Storage cleanup in progress",
			ObservedGeneration: app.Generation,
		},
	)
}

func SetStorageCleanupFailed(
	app *forgev1alpha1.Application,
	err error,
) {
	metav1.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:    StorageReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonBucketCleanupFailed,
			Message: fmt.Sprintf("Storage cleanup failed: %v", err),
			ObservedGeneration: app.Generation,
		},
	)
}