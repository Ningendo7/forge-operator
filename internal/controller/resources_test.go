package controller

import (
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestDesiredIngressUsesConfiguredValues(t *testing.T) {
	r := &ApplicationReconciler{}
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	pathType := networkingv1.PathTypePrefix
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{
		Host:        "example.com",
		Path:        "/api",
		PathType:    &pathType,
		ClassName:   stringPtr("nginx"),
		Annotations: map[string]string{"cert-manager.io/cluster-issuer": "letsencrypt"},
	}

	ing := r.desiredIngress(app)
	if ing.Name != app.Name {
		t.Fatalf("expected ingress name %q, got %q", app.Name, ing.Name)
	}
	if len(ing.Spec.Rules) != 1 {
		t.Fatalf("expected one ingress rule, got %d", len(ing.Spec.Rules))
	}
	if ing.Spec.Rules[0].Host != app.Spec.Ingress.Host {
		t.Fatalf("expected ingress host %q, got %q", app.Spec.Ingress.Host, ing.Spec.Rules[0].Host)
	}
	if ing.Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Path != app.Spec.Ingress.Path {
		t.Fatalf("expected ingress path %q, got %q", app.Spec.Ingress.Path, ing.Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Path)
	}
	if ing.Spec.TLS != nil {
		t.Fatalf("expected no TLS block by default")
	}
}

func TestDesiredSecretUsesConfiguredValues(t *testing.T) {
	r := &ApplicationReconciler{}
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		Name:       "custom-secret",
		StringData: map[string]string{"API_KEY": "abc123"},
		Type:       corev1.SecretTypeOpaque,
	}

	secret := r.desiredSecret(app)
	if secret.Name != "custom-secret" {
		t.Fatalf("expected secret name custom-secret, got %q", secret.Name)
	}
	if secret.StringData["API_KEY"] != "abc123" {
		t.Fatalf("expected secret data API_KEY to be present")
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("expected secret type %q, got %q", corev1.SecretTypeOpaque, secret.Type)
	}
}

func TestDesiredPDBUsesConfiguredBudgets(t *testing.T) {
	r := &ApplicationReconciler{}
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	minAvailable := intstr.FromInt(1)
	maxUnavailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{
		MinAvailable:   &minAvailable,
		MaxUnavailable: &maxUnavailable,
	}

	pdb := r.desiredPDB(app)
	if pdb.Name != app.Name {
		t.Fatalf("expected pdb name %q, got %q", app.Name, pdb.Name)
	}
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.String() != "1" {
		t.Fatalf("expected minAvailable=1, got %#v", pdb.Spec.MinAvailable)
	}
	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.String() != "1" {
		t.Fatalf("expected maxUnavailable=1, got %#v", pdb.Spec.MaxUnavailable)
	}
}

func stringPtr(value string) *string {
	return &value
}
