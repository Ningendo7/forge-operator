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

	MaxErrorMessageLength = 250
)

func truncateMessage(
	message string, 
	maxLength int,
) string {
	if len(message) > maxLength {
		return message[:maxLength-3] + "..."
	}
	return message
}

func SetStorageReady(app *forgev1alpha1.Application, 
	message string,
) {

	if app == nil {
		return
	}

	app.Status.Storage = storageStatus

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

	if app == nil {
		return
	}
	msg := "Storage configuration failed"
	if err != nil {
		msg = fmt.Sprintf("Storage configuration failed: %v", truncateMessage(err.Error(), MaxErrorMessageLength))
	}
	metav1.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:    StorageReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonBucketConfigurationFailed,
			Message: msg,
			ObservedGeneration: app.Generation,
		},
	)
}

func SetStorageCleanupInProgress(
	app *forgev1alpha1.Application,
) {

	if app == nil {
		return
	}

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
	if app == nil {
		return
	}

	msg := "Storage cleanup failed"
	if err != nil {
		msg = fmt.Sprintf("Storage cleanup failed: %v", truncateMessage(err.Error(), MaxErrorMessageLength))
	}

	metav1.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:    StorageReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonBucketCleanupFailed,
			Message: msg,
			ObservedGeneration: app.Generation,
		},
	)
}