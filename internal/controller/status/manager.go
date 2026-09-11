package status

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	TypeReady       = "Ready"
	TypeProgressing = "Progressing"
	TypeDegraded    = "Degraded"

	ReasonReconciling = "Reconciling"
	ReasonAvailable   = "ReconcileSuccess"
	ReasonFailed      = "ReconcileFailed"
)

type StatusManager struct {
	client client.Client
	// apiReader reads directly from the API server, bypassing the manager's
	// informer cache -- used only by UpdateStatus to re-fetch a fresh copy
	// of the Application after a conflict. The cached client (client field
	// above) is correct for every other read in this operator; this one
	// deliberately isn't cached, since retrying a conflict against the same
	// stale cache read that caused it would just reproduce the same
	// conflict.
	apiReader client.Reader
}

func NewStatusManager(
	c client.Client,
	apiReader client.Reader,
) *StatusManager {

	return &StatusManager{
		client:    c,
		apiReader: apiReader,
	}
}

func (s *StatusManager) SetReconciling(
	ctx context.Context,
	application *forgev1alpha1.Application,
	message string,
) error {

	progressingChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonReconciling,
		Message:            message,
		ObservedGeneration: application.Generation,
	})

	readyChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonReconciling,
		Message:            "Reconciliation in progress",
		ObservedGeneration: application.Generation,
	})

	if !progressingChanged && !readyChanged {
		// Nothing actually changed -- skip the write entirely. Without
		// this, Reconcile calling SetReconciling unconditionally on every
		// single pass (including a routine no-op, like the periodic
		// storageResyncInterval wakeup finding everything already fine)
		// does a full Status().Update() every time regardless -- wasted API
		// traffic, and more surface area for the resourceVersion-conflict
		// race UpdateStatus's own retry below exists to recover from in the
		// first place.
		return nil
	}

	return s.UpdateStatus(ctx, application)

}

func (s *StatusManager) SetReady(
	ctx context.Context,
	application *forgev1alpha1.Application,
	message string,
) error {

	readyChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonAvailable,
		Message:            message,
		ObservedGeneration: application.Generation,
	})

	progressingChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonAvailable,
		Message:            "Application is up to date and ready",
		ObservedGeneration: application.Generation,
	})

	degradedChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonAvailable,
		Message:            "No errors observed",
		ObservedGeneration: application.Generation,
	})

	if !readyChanged && !progressingChanged && !degradedChanged {
		return nil
	}

	return s.UpdateStatus(ctx, application)
}

func (s *StatusManager) SetFailed(
	ctx context.Context,
	application *forgev1alpha1.Application,
	err error,
) error {

	degradedChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonFailed,
		Message:            err.Error(),
		ObservedGeneration: application.Generation,
	})

	readyChanged := meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:               TypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonFailed,
		Message:            fmt.Sprintf("Reconciliation failed: %v", err),
		ObservedGeneration: application.Generation,
	})

	if !degradedChanged && !readyChanged {
		return nil
	}

	return s.UpdateStatus(ctx, application)
}

func (s *StatusManager) UpdateStatus(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)

	application.Status.ObservedGeneration = application.Generation

	// Captured before the retry loop: a conflict retry re-fetches
	// application fresh from the API server (see below), which would
	// otherwise discard the in-memory conditions this call actually wants
	// to persist.
	desiredConditions := application.Status.Conditions
	desiredObservedGeneration := application.Status.ObservedGeneration

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		updateErr := s.client.Status().Update(ctx, application)
		if updateErr == nil {
			return nil
		}
		if !apierrors.IsConflict(updateErr) {
			return updateErr
		}

		// application.ResourceVersion was stale -- almost always because it
		// was Get() 'd from the manager's informer cache, which can briefly
		// lag the API server's true state right after a fast preceding
		// write. Re-fetch via apiReader (bypasses that same cache --
		// retrying against it again would just reproduce the same
		// conflict), then re-apply the conditions this call actually wants
		// to persist onto that fresh copy before RetryOnConflict tries
		// again.
		fresh := &forgev1alpha1.Application{}
		if getErr := s.apiReader.Get(ctx, client.ObjectKeyFromObject(application), fresh); getErr != nil {
			return getErr
		}
		fresh.Status.Conditions = desiredConditions
		fresh.Status.ObservedGeneration = desiredObservedGeneration
		*application = *fresh
		return updateErr
	})

	if err != nil {
		logger.Error(err, "Failed to update Application status", "name", application.Name)
		return err
	}

	logger.Info("Successfully updated Application status", "name", application.Name)
	return nil
}
