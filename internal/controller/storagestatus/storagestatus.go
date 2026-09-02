// Package storagestatus is the single source of truth for the StorageReady
// condition, shared by the AWS and Akamai backends so their Reasons don't drift.
package storagestatus

import (
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StorageReady = "StorageReady"

	ReasonBucketConfigured          = "BucketConfigured"
	ReasonBucketConfigurationFailed = "BucketConfigurationFailed"
	ReasonBucketNotOwned            = "BucketNotOwned"
	ReasonBucketCleanup             = "BucketCleanup"
	ReasonBucketCleanupFailed       = "BucketCleanupFailed"

	MaxErrorMessageLength = 250
)

// SetNotOwned marks storage as blocked because a bucket with the desired
// name already exists but wasn't created by this operator for this
// Application. Distinct from SetNotReady so this doesn't look like a
// transient/retryable failure -- it needs a human to rename the bucket spec
// or resolve the conflict, not a reconcile retry.
func SetNotOwned(
	app *forgev1alpha1.Application,
	err error,
) {
	if app == nil {
		return
	}
	msg := "Bucket already exists and is not owned by forge-operator"
	if err != nil {
		msg = truncateMessage(err.Error(), MaxErrorMessageLength)
	}
	apiMeta.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:               StorageReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonBucketNotOwned,
			Message:            msg,
			ObservedGeneration: app.Generation,
		},
	)
}

func truncateMessage(
	message string,
	maxLength int,
) string {
	if len(message) > maxLength {
		return message[:maxLength-3] + "..."
	}
	return message
}

// SetReady marks storage as provisioned and records the given status payload.
func SetReady(
	app *forgev1alpha1.Application,
	storageStatus *forgev1alpha1.StorageStatus,
	message string,
) {

	if app == nil {
		return
	}

	app.Status.Storage = storageStatus

	apiMeta.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:               StorageReady,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonBucketConfigured,
			Message:            message,
			ObservedGeneration: app.Generation,
		},
	)
}

// SetNotReady marks storage provisioning as failed.
func SetNotReady(
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
	apiMeta.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:               StorageReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonBucketConfigurationFailed,
			Message:            msg,
			ObservedGeneration: app.Generation,
		},
	)
}

// SetCleanupInProgress marks storage teardown as underway, e.g. while a
// finalizer is deleting the backing bucket.
func SetCleanupInProgress(
	app *forgev1alpha1.Application,
) {

	if app == nil {
		return
	}

	apiMeta.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:               StorageReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonBucketCleanup,
			Message:            "Storage cleanup in progress",
			ObservedGeneration: app.Generation,
		},
	)
}

// SetCleanupFailed marks storage teardown as failed.
func SetCleanupFailed(
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

	apiMeta.SetStatusCondition(
		&app.Status.Conditions,
		metav1.Condition{
			Type:               StorageReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonBucketCleanupFailed,
			Message:            msg,
			ObservedGeneration: app.Generation,
		},
	)
}
