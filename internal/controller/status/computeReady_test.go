package status

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newComputeReadyScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)
	return scheme
}

func readyDeployment() *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{UpdatedReplicas: 1, AvailableReplicas: 1, ReadyReplicas: 1},
	}
}

func readyService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
	}
}

func TestEvaluateComputeReadiness_NotReadyWhenServiceMissing(t *testing.T) {
	app := newTestApplication()
	scheme := newComputeReadyScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyDeployment()).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when Service is missing")
	}
	if msg == "" {
		t.Fatalf("expected a message explaining why not ready")
	}
}

func TestEvaluateComputeReadiness_NotReadyWhenDeploymentNotReady(t *testing.T) {
	app := newTestApplication()
	scheme := newComputeReadyScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyService()).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when Deployment does not exist")
	}
}

func TestEvaluateComputeReadiness_NotReadyWhenIngressEnabledButNotReady(t *testing.T) {
	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: "example.com"}
	scheme := newComputeReadyScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyService(), readyDeployment()).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when Ingress is enabled but not found, msg=%q", msg)
	}
}

func TestEvaluateComputeReadiness_SkipsIngressCheckWhenNotConfigured(t *testing.T) {
	app := newTestApplication()
	scheme := newComputeReadyScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyService(), readyDeployment()).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true when Ingress is not configured, got msg=%q", msg)
	}
}

func TestEvaluateComputeReadiness_NotReadyWhenAutoscalingEnabledButNotReady(t *testing.T) {
	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}
	scheme := newComputeReadyScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyService(), readyDeployment()).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when Autoscaling is enabled but HPA not found, msg=%q", msg)
	}
}

func TestEvaluateComputeReadiness_NotReadyWhenPDBEnabledButNotReady(t *testing.T) {
	app := newTestApplication()
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}
	scheme := newComputeReadyScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyService(), readyDeployment()).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when PDB is enabled but not found, msg=%q", msg)
	}
}

func TestEvaluateComputeReadiness_ReadyWhenAllEnabledResourcesAreReady(t *testing.T) {
	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: "example.com"}
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}

	scheme := newComputeReadyScheme()

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "203.0.113.5"}},
			},
		},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-hpa", Namespace: "default"},
	}
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-pdb", Namespace: "default"},
		Status: policyv1.PodDisruptionBudgetStatus{
			DisruptionsAllowed: 1,
			CurrentHealthy:     1,
			DesiredHealthy:     1,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(readyService(), readyDeployment(), ingress, hpa, pdb).
		Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.EvaluateComputeReadiness(context.Background(), app)
	if err != nil {
		t.Fatalf("EvaluateComputeReadiness returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true when all enabled resources are ready, got msg=%q", msg)
	}
}
