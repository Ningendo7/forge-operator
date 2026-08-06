package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDesiredPDB_DefaultsToNoBudget(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	pdb := r.desiredPDB(app)

	if pdb.Name != "demo-app-pdb" {
		t.Fatalf("expected pdb name %q, got %q", "demo-app-pdb", pdb.Name)
	}
	if pdb.Spec.MinAvailable != nil {
		t.Fatalf("expected no minAvailable by default, got %#v", pdb.Spec.MinAvailable)
	}
	if pdb.Spec.MaxUnavailable != nil {
		t.Fatalf("expected no maxUnavailable by default, got %#v", pdb.Spec.MaxUnavailable)
	}
}

func TestDesiredPDB_UsesConfiguredMinAvailable(t *testing.T) {
	app := newTestApplication()

	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{
		MinAvailable: &minAvailable,
	}

	r := &ApplicationReconciler{}
	pdb := r.desiredPDB(app)

	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.String() != "1" {
		t.Fatalf("expected minAvailable=1, got %#v", pdb.Spec.MinAvailable)
	}
	if pdb.Spec.MaxUnavailable != nil {
		t.Fatalf("expected no maxUnavailable when minAvailable is set, got %#v", pdb.Spec.MaxUnavailable)
	}
}

func TestDesiredPDB_UsesConfiguredMaxUnavailable(t *testing.T) {
	app := newTestApplication()

	maxUnavailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{
		MaxUnavailable: &maxUnavailable,
	}

	r := &ApplicationReconciler{}
	pdb := r.desiredPDB(app)

	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.String() != "1" {
		t.Fatalf("expected maxUnavailable=1, got %#v", pdb.Spec.MaxUnavailable)
	}
}

func TestDesiredPDB_SetsSelectorLabels(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	pdb := r.desiredPDB(app)

	if pdb.Spec.Selector.MatchLabels["app"] != app.Name {
		t.Fatalf("expected selector label 'app' to be %q, got %q", app.Name, pdb.Spec.Selector.MatchLabels["app"])
	}
}

func TestReconcilePDB_CreatesPDB(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	app := newTestApplication()
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcilePDB(context.Background(), app); err != nil {
		t.Fatalf("reconcilePDB returned error: %v", err)
	}

	pdb := &policyv1.PodDisruptionBudget{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-pdb", Namespace: "default"}, pdb); err != nil {
		t.Fatalf("failed to get PodDisruptionBudget: %v", err)
	}
	if pdb.Spec.MinAvailable.String() != "1" {
		t.Errorf("expected minAvailable 1, got %v", pdb.Spec.MinAvailable)
	}
}

func TestReconcilePDB_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	app := newTestApplication()
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcilePDB(context.Background(), app); err != nil {
		t.Fatalf("first reconcilePDB returned error: %v", err)
	}
	if err := r.reconcilePDB(context.Background(), app); err != nil {
		t.Fatalf("second reconcilePDB returned error: %v", err)
	}

	pdb := &policyv1.PodDisruptionBudget{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-pdb", Namespace: "default"}, pdb); err != nil {
		t.Fatalf("failed to get PodDisruptionBudget after second reconciliation: %v", err)
	}
}

func TestReconcilePDB_DeletesWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	app := newTestApplication()
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcilePDB(context.Background(), app); err != nil {
		t.Fatalf("failed to create PDB: %v", err)
	}

	app.Spec.PDB = nil
	if err := r.reconcilePDB(context.Background(), app); err != nil {
		t.Fatalf("reconcilePDB returned error on disable: %v", err)
	}

	pdb := &policyv1.PodDisruptionBudget{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-pdb", Namespace: "default"}, pdb)
	if err == nil {
		t.Fatalf("expected PodDisruptionBudget to be deleted, but it still exists")
	}
}

func TestReconcilePDB_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	app := newTestApplication()
	app.ObjectMeta.UID = "12345"
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcilePDB(context.Background(), app); err != nil {
		t.Fatalf("reconcilePDB returned error: %v", err)
	}

	pdb := &policyv1.PodDisruptionBudget{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-pdb", Namespace: "default"}, pdb); err != nil {
		t.Fatalf("failed to get PodDisruptionBudget: %v", err)
	}

	if len(pdb.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(pdb.OwnerReferences))
	}
	if pdb.OwnerReferences[0].Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, pdb.OwnerReferences[0].Name)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcilePDB_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	app := newTestApplication()
	minAvailable := intstr.FromInt(1)
	app.Spec.PDB = &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable}

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.reconcilePDB(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcilePDB, got nil")
	}
}
