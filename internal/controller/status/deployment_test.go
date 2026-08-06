package status

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newDeploymentTestClient(objs ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...)
}

func TestIsDeploymentReady_NotFound(t *testing.T) {
	fakeClient := newDeploymentTestClient().Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("expected nil error for not-found deployment, got %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when deployment does not exist")
	}
	if msg == "" {
		t.Fatalf("expected a message explaining why not ready")
	}
}

func TestIsDeploymentReady_ObservedGenerationLagsSpec(t *testing.T) {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 1},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when observed generation lags spec generation")
	}
}

func TestIsDeploymentReady_ProgressingConditionFalse(t *testing.T) {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Message: "progress deadline exceeded"},
			},
		},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when Progressing condition is False")
	}
	if msg == "" {
		t.Fatalf("expected a message describing the rollout issue")
	}
}

func TestIsDeploymentReady_ReplicaFailureConditionTrue(t *testing.T) {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue, Message: "failed to create pod"},
			},
		},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when ReplicaFailure condition is True")
	}
}

func TestIsDeploymentReady_UpdatedReplicasBelowDesired(t *testing.T) {
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{UpdatedReplicas: 1},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when updated replicas is below desired")
	}
}

func TestIsDeploymentReady_AvailableReplicasBelowDesired(t *testing.T) {
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 1},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when available replicas is below desired")
	}
}

func TestIsDeploymentReady_ReadyReplicasBelowDesired(t *testing.T) {
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 3, ReadyReplicas: 1},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when ready replicas is below desired")
	}
}

func TestIsDeploymentReady_FullyReady(t *testing.T) {
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 3, ReadyReplicas: 3},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true when all replica counts satisfy desired, got msg=%q", msg)
	}
}

func TestIsDeploymentReady_DefaultsDesiredReplicasToOneWhenUnset(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Status:     appsv1.DeploymentStatus{UpdatedReplicas: 1, AvailableReplicas: 1, ReadyReplicas: 1},
	}
	fakeClient := newDeploymentTestClient(dep).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsDeploymentReady(context.Background(), "default", "demo-app-deployment")
	if err != nil {
		t.Fatalf("IsDeploymentReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true with default desired replicas of 1")
	}
}
