package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func stringPtr(value string) *string {
	return &value
}

func TestDesiredIngress_UsesConfiguredValues(t *testing.T) {
	app := newTestApplication()

	pathType := networkingv1.PathTypePrefix
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{
		Host:        testExampleHost,
		Path:        "/api",
		PathType:    &pathType,
		ClassName:   stringPtr("nginx"),
		Annotations: map[string]string{"cert-manager.io/cluster-issuer": "letsencrypt"},
	}

	r := &ApplicationReconciler{}
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
	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != "nginx" {
		t.Fatalf("expected ingress class name %q, got %v", "nginx", ing.Spec.IngressClassName)
	}
	if ing.Annotations["cert-manager.io/cluster-issuer"] != "letsencrypt" {
		t.Fatalf("expected annotation to be set, got %v", ing.Annotations)
	}
}

func TestDesiredIngress_PropagatesTLS(t *testing.T) {
	// Guards against the TLS field being silently dropped: it's accepted
	// by the CRD but was never read by desiredIngress, so the generated
	// Ingress never carried a spec.tls block -- cert-manager's
	// ingress-shim needs tls[].secretName to know where to write an
	// issued certificate, so this meant TLS silently never provisioned
	// no matter what a user configured, with no error anywhere.
	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{
		Host: testExampleHost,
		TLS: []networkingv1.IngressTLS{{
			Hosts:      []string{testExampleHost},
			SecretName: "example-tls",
		}},
	}

	r := &ApplicationReconciler{}
	ing := r.desiredIngress(app)

	if len(ing.Spec.TLS) != 1 {
		t.Fatalf("expected 1 TLS entry, got %d", len(ing.Spec.TLS))
	}
	if ing.Spec.TLS[0].SecretName != "example-tls" {
		t.Fatalf("expected TLS secretName %q, got %q", "example-tls", ing.Spec.TLS[0].SecretName)
	}
	if len(ing.Spec.TLS[0].Hosts) != 1 || ing.Spec.TLS[0].Hosts[0] != testExampleHost {
		t.Fatalf("expected TLS hosts %v, got %v", []string{testExampleHost}, ing.Spec.TLS[0].Hosts)
	}
}

func TestDesiredIngress_OmitsTLSWhenUnset(t *testing.T) {
	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost}

	r := &ApplicationReconciler{}
	ing := r.desiredIngress(app)

	if len(ing.Spec.TLS) != 0 {
		t.Fatalf("expected no TLS entries when spec.ingress.tls is unset, got %v", ing.Spec.TLS)
	}
}

func TestDesiredIngress_SetsLabels(t *testing.T) {
	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost}

	r := &ApplicationReconciler{}
	ing := r.desiredIngress(app)

	if ing.Labels["app"] != app.Name {
		t.Fatalf("expected label 'app' to be %q, got %q", app.Name, ing.Labels["app"])
	}
}

func TestReconcileIngress_CreatesIngress(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost, Path: "/"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileIngress(context.Background(), app); err != nil {
		t.Fatalf("reconcileIngress returned error: %v", err)
	}

	ing := &networkingv1.Ingress{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, ing); err != nil {
		t.Fatalf("failed to get Ingress: %v", err)
	}
	if ing.Spec.Rules[0].Host != testExampleHost {
		t.Errorf("expected ingress host example.com, got %q", ing.Spec.Rules[0].Host)
	}
}

func TestReconcileIngress_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost, Path: "/"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileIngress(context.Background(), app); err != nil {
		t.Fatalf("first reconcileIngress returned error: %v", err)
	}
	if err := r.reconcileIngress(context.Background(), app); err != nil {
		t.Fatalf("second reconcileIngress returned error: %v", err)
	}

	ing := &networkingv1.Ingress{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, ing); err != nil {
		t.Fatalf("failed to get Ingress after second reconciliation: %v", err)
	}
}

func TestReconcileIngress_DeletesWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost, Path: "/"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileIngress(context.Background(), app); err != nil {
		t.Fatalf("failed to create ingress: %v", err)
	}

	app.Spec.Ingress = nil
	if err := r.reconcileIngress(context.Background(), app); err != nil {
		t.Fatalf("reconcileIngress returned error on disable: %v", err)
	}

	ing := &networkingv1.Ingress{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, ing)
	if err == nil {
		t.Fatalf("expected Ingress to be deleted, but it still exists")
	}
}

func TestReconcileIngress_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	app := newTestApplication()
	app.UID = "12345"
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost, Path: "/"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileIngress(context.Background(), app); err != nil {
		t.Fatalf("reconcileIngress returned error: %v", err)
	}

	ing := &networkingv1.Ingress{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, ing); err != nil {
		t.Fatalf("failed to get Ingress: %v", err)
	}

	if len(ing.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(ing.OwnerReferences))
	}
	if ing.OwnerReferences[0].Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, ing.OwnerReferences[0].Name)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcileIngress_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Ingress = &forgev1alpha1.IngressSpec{Host: testExampleHost, Path: "/"}

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.reconcileIngress(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcileIngress, got nil")
	}
}
