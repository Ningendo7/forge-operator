package status

import (
	"context"
	"testing"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newPDBTestClient(objs ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = policyv1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...)
}

func TestIsPDBReady_NotFound(t *testing.T) {
	fakeClient := newPDBTestClient().Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsPDBReady(context.Background(), "default", "demo-app-pdb")
	if err != nil {
		t.Fatalf("expected nil error for not-found PDB, got %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when PDB does not exist")
	}
	if msg == "" {
		t.Fatalf("expected a message explaining why not ready")
	}
}

func TestIsPDBReady_UnhealthyWhenNoDisruptionsAllowedAndBelowDesired(t *testing.T) {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-pdb", Namespace: "default"},
		Status: policyv1.PodDisruptionBudgetStatus{
			DisruptionsAllowed: 0,
			CurrentHealthy:     1,
			DesiredHealthy:     3,
		},
	}
	fakeClient := newPDBTestClient(pdb).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsPDBReady(context.Background(), "default", "demo-app-pdb")
	if err != nil {
		t.Fatalf("IsPDBReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when unhealthy, got msg=%q", msg)
	}
}

func TestIsPDBReady_HealthyWhenDisruptionsAllowed(t *testing.T) {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-pdb", Namespace: "default"},
		Status: policyv1.PodDisruptionBudgetStatus{
			DisruptionsAllowed: 1,
			CurrentHealthy:     3,
			DesiredHealthy:     3,
		},
	}
	fakeClient := newPDBTestClient(pdb).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsPDBReady(context.Background(), "default", "demo-app-pdb")
	if err != nil {
		t.Fatalf("IsPDBReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true, got msg=%q", msg)
	}
}

func TestIsPDBReady_HealthyWhenCurrentMeetsDesiredDespiteNoDisruptionsAllowed(t *testing.T) {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-pdb", Namespace: "default"},
		Status: policyv1.PodDisruptionBudgetStatus{
			DisruptionsAllowed: 0,
			CurrentHealthy:     3,
			DesiredHealthy:     3,
		},
	}
	fakeClient := newPDBTestClient(pdb).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsPDBReady(context.Background(), "default", "demo-app-pdb")
	if err != nil {
		t.Fatalf("IsPDBReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true when current healthy meets desired, got msg=%q", msg)
	}
}
