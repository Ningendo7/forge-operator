package status

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newIngressTestClient(objs ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = networkingv1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...)
}

func TestIsIngressReady_NotFound(t *testing.T) {
	fakeClient := newIngressTestClient().Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsIngressReady(context.Background(), "default", "demo-app")
	if err != nil {
		t.Fatalf("expected nil error for not-found ingress, got %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when ingress does not exist")
	}
	if msg == "" {
		t.Fatalf("expected a message explaining why not ready")
	}
}

func TestIsIngressReady_PendingWhenLoadBalancerNil(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
	}
	fakeClient := newIngressTestClient(ing).Build()
	s := NewStatusManager(fakeClient)

	ready, _, err := s.IsIngressReady(context.Background(), "default", "demo-app")
	if err != nil {
		t.Fatalf("IsIngressReady returned error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false when LoadBalancer status is nil")
	}
}

func TestIsIngressReady_ReadyWithIP(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "203.0.113.5"}},
			},
		},
	}
	fakeClient := newIngressTestClient(ing).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsIngressReady(context.Background(), "default", "demo-app")
	if err != nil {
		t.Fatalf("IsIngressReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true when IP is assigned, got msg=%q", msg)
	}
}

func TestIsIngressReady_ReadyWithHostname(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: "lb.example.com"}},
			},
		},
	}
	fakeClient := newIngressTestClient(ing).Build()
	s := NewStatusManager(fakeClient)

	ready, msg, err := s.IsIngressReady(context.Background(), "default", "demo-app")
	if err != nil {
		t.Fatalf("IsIngressReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true when Hostname is assigned, got msg=%q", msg)
	}
}
