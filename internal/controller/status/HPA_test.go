package status

import (
	"context"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newHPATestClient(objs ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = autoscalingv2.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...)
}

func TestIsHPAReady_NotFound(t *testing.T) {
	fakeClient := newHPATestClient().Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsHPAReady(context.Background(), "default", "demo-app-hpa")
	if err != nil {
		t.Fatalf("expected nil error for not-found HPA, got %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when HPA does not exist")
	}
	if msg == "" {
		t.Fatalf("expected a message explaining why not ready")
	}
}

func TestIsHPAReady_ObservedGenerationLagsSpec(t *testing.T) {
	observed := int64(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-hpa", Namespace: "default", Generation: 2},
		Status:     autoscalingv2.HorizontalPodAutoscalerStatus{ObservedGeneration: &observed},
	}
	fakeClient := newHPATestClient(hpa).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsHPAReady(context.Background(), "default", "demo-app-hpa")
	if err != nil {
		t.Fatalf("IsHPAReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when observed generation lags spec generation")
	}
}

func TestIsHPAReady_AbleToScaleFalse(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-hpa", Namespace: "default"},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: autoscalingv2.AbleToScale, Status: "False", Message: "failed to get metrics"},
			},
		},
	}
	fakeClient := newHPATestClient(hpa).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsHPAReady(context.Background(), "default", "demo-app-hpa")
	if err != nil {
		t.Fatalf("IsHPAReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when AbleToScale is False")
	}
	if msg == "" {
		t.Fatalf("expected a message describing the scaling issue")
	}
}

func TestIsHPAReady_ReadyWhenNoIssues(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-hpa", Namespace: "default"},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 2,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: autoscalingv2.AbleToScale, Status: "True"},
			},
		},
	}
	fakeClient := newHPATestClient(hpa).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsHPAReady(context.Background(), "default", "demo-app-hpa")
	if err != nil {
		t.Fatalf("IsHPAReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true, got msg=%q", msg)
	}
}
