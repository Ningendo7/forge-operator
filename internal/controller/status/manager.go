package status

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (

	TypeReady    = "Ready"
	TypeProgressing = "Progressing"
	TypeDegraded = "Degraded"

	ReasonReconciling = "Reconciling"
	ReasonAvailable = "ReconcileSuccess"
	ReasonFailed = "ReconcileFailed"
)

type StatusManager struct {
	client client.Client
}

func NewStatusManager(
	c client.Client,
) *StatusManager {

	return &StatusManager{
		client: c,
	}
}

func (s *StatusManager) SetReconciling(
	ctx context.Context,
	application *forgev1alpha1.Application,
	message string,
) error {

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeProgressing,
		Status:  	  metav1.ConditionTrue,
		Reason:  	  ReasonReconciling,
		Message: 	  message,
		ObservedGeneration: application.Generation,
	})

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeReady,
		Status:  	  metav1.ConditionFalse,
		Reason:  	  ReasonReconciling,
		Message: 	  "Reconciliation in progress",
		ObservedGeneration: application.Generation,
	})

	return s.updateStatus(ctx, application)

}

func (s *StatusManager) SetReady(
	ctx context.Context,
	application *forgev1alpha1.Application,
	message string,
) error {

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeReady,
		Status:  	  metav1.ConditionTrue,
		Reason:  	  ReasonAvailable,
		Message: 	  message,
		ObservedGeneration: application.Generation,
	})

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeProgressing,
		Status:  	  metav1.ConditionFalse,
		Reason:  	  ReasonAvailable,
		Message: 	  "Application is up to date and ready",
		ObservedGeneration: application.Generation,
	})

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeDegraded,
		Status:  	  metav1.ConditionFalse,
		Reason:  	  ReasonAvailable,
		Message: 	  "No errors observed",
		ObservedGeneration: application.Generation,
	})

	return s.updateStatus(ctx, application)
}

func (s *StatusManager) SetFailed(
	ctx context.Context,
	application *forgev1alpha1.Application,
	err error,
) error {

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeDegraded,
		Status:  	  metav1.ConditionTrue,
		Reason:  	  ReasonFailed,
		Message: 	  err.Error(),
		ObservedGeneration: application.Generation,
	})

	meta.SetStatusCondition(&application.Status.Conditions, metav1.Condition{
		Type:    	  TypeReady,
		Status:  	  metav1.ConditionFalse,
		Reason:  	  ReasonFailed,
		Message: 	  fmt.Sprintf("Reconciliation failed: %v", err),
		ObservedGeneration: application.Generation,
	})

	return s.updateStatus(ctx, application)
}

func (s *StatusManager) updateStatus(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)

	if err := s.client.Status().Update(ctx, application); err != nil {
		logger.Error(err, "Failed to update Application status", "name", application.Name)
		return err
	}

	logger.Info("Successfully updated Application status", "name", application.Name)
	return nil
}