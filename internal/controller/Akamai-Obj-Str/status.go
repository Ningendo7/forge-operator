package akamaiobjstr

import (
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

)

const (
	StorageReady = "StorageReady"

	ReasonBucketProvisioned = "BucketProvisioned"
	ReasonProvisioningFailed = "ProvisioningFailed"
	// ReasonBucketCleanup = "BucketCleanup"
	// ReasonBucketCleanupFailed = "BucketCleanupFailed"

	// MaxErrorMessageLength = 250
)

func SetStorageReady(
	application *forgev1alpha1.Application,
	status *forgev1alpha1.StorageStatus,
	message string,
) {

	application.Status.Storage = status

	condition := metav1.Condition{
		Type:               StorageReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonBucketProvisioned,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	updateCondition(application, condition)
}

func SetStorageNotReady(
	application *forgev1alpha1.Application,
	err error,
) {

	condition := metav1.Condition{
		Type:               StorageReady,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonProvisioningFailed,
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	}

	updateCondition(application, condition)
}	

func updateCondition(
	application *forgev1alpha1.Application,
	newCond metav1.Condition,
) {
	for i, cond := range application.Status.Conditions {
		if cond.Type == newCond.Type {
			application.Status.Conditions[i] = newCond
			return
		}
	}
	application.Status.Conditions = append(application.Status.Conditions, newCond)
}