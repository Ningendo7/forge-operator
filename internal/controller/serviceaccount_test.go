package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestShouldCreateServiceAccount(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		spec     *forgev1alpha1.ServiceAccountSpec
		expected bool
	}{
		{name: "nil spec defaults to true", spec: nil, expected: true},
		{name: "nil Create field, no Name, defaults to true", spec: &forgev1alpha1.ServiceAccountSpec{}, expected: true},
		{
			name: "nil Create field but Name set defaults to false -- an existing ServiceAccount to use, not one we own",
			spec: &forgev1alpha1.ServiceAccountSpec{Name: testCustomSAName}, expected: false,
		},
		{name: "Create true", spec: &forgev1alpha1.ServiceAccountSpec{Create: &trueVal}, expected: true},
		{name: "Create true even with Name also set", spec: &forgev1alpha1.ServiceAccountSpec{Create: &trueVal, Name: testCustomSAName}, expected: true},
		{name: "Create false", spec: &forgev1alpha1.ServiceAccountSpec{Create: &falseVal}, expected: false},
		{name: "Create false even with Name also set", spec: &forgev1alpha1.ServiceAccountSpec{Create: &falseVal, Name: testCustomSAName}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApplication()
			app.Spec.ServiceAccount = tt.spec

			if got := shouldCreateServiceAccount(app); got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestServiceAccountNameFor(t *testing.T) {
	tests := []struct {
		name     string
		spec     *forgev1alpha1.ServiceAccountSpec
		expected string
	}{
		{name: "defaults to app name with -sa suffix", spec: nil, expected: testSAName},
		{name: "uses configured name", spec: &forgev1alpha1.ServiceAccountSpec{Name: testCustomSAName}, expected: testCustomSAName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApplication()
			app.Spec.ServiceAccount = tt.spec

			if got := serviceAccountNameFor(app); got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestPodServiceAccountName(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		spec     *forgev1alpha1.ServiceAccountSpec
		expected string
	}{
		{name: "unset spec: generated default name, operator creates it", spec: nil, expected: testSAName},
		{
			name:     "Name only, Create unset: pod still gets the name even though the operator doesn't own it",
			spec:     &forgev1alpha1.ServiceAccountSpec{Name: testCustomSAName},
			expected: testCustomSAName,
		},
		{
			name:     "Create false, Name unset: nothing to reference",
			spec:     &forgev1alpha1.ServiceAccountSpec{Create: &falseVal},
			expected: "",
		},
		{
			name:     "Create false, Name set: still wired into the pod spec, just not owned",
			spec:     &forgev1alpha1.ServiceAccountSpec{Create: &falseVal, Name: testCustomSAName},
			expected: testCustomSAName,
		},
		{
			name:     "Create true, Name unset: generated default name",
			spec:     &forgev1alpha1.ServiceAccountSpec{Create: &trueVal},
			expected: testSAName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApplication()
			app.Spec.ServiceAccount = tt.spec

			if got := podServiceAccountName(app); got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestDesiredServiceAccount_UsesConfiguredName(t *testing.T) {
	app := newTestApplication()
	app.Spec.ServiceAccount = &forgev1alpha1.ServiceAccountSpec{Name: testCustomSAName}

	r := &ApplicationReconciler{}
	sa := r.desiredServiceAccount(app)

	if sa.Name != testCustomSAName {
		t.Fatalf("expected service account name custom-sa, got %q", sa.Name)
	}
	if sa.Labels["app"] != app.Name {
		t.Fatalf("expected label 'app' to be %q, got %q", app.Name, sa.Labels["app"])
	}
}

func TestReconcileServiceAccount_CreatesServiceAccount(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileServiceAccount(context.Background(), app); err != nil {
		t.Fatalf("reconcileServiceAccount returned error: %v", err)
	}

	sa := &corev1.ServiceAccount{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testSAName, Namespace: testNamespace}, sa); err != nil {
		t.Fatalf("failed to get ServiceAccount: %v", err)
	}
}

func TestReconcileServiceAccount_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileServiceAccount(context.Background(), app); err != nil {
		t.Fatalf("first reconcileServiceAccount returned error: %v", err)
	}
	if err := r.reconcileServiceAccount(context.Background(), app); err != nil {
		t.Fatalf("second reconcileServiceAccount returned error: %v", err)
	}

	sa := &corev1.ServiceAccount{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testSAName, Namespace: testNamespace}, sa); err != nil {
		t.Fatalf("failed to get ServiceAccount after second reconciliation: %v", err)
	}
}

func TestReconcileServiceAccount_SkipsWhenCreateIsFalse(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	falseVal := false
	app := newTestApplication()
	app.Spec.ServiceAccount = &forgev1alpha1.ServiceAccountSpec{
		Name:   "user-managed-sa",
		Create: &falseVal,
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileServiceAccount(context.Background(), app); err != nil {
		t.Fatalf("reconcileServiceAccount returned error: %v", err)
	}

	sa := &corev1.ServiceAccount{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "user-managed-sa", Namespace: testNamespace}, sa)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected ServiceAccount to not be created, got err=%v", err)
	}
}

func TestReconcileServiceAccount_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.UID = "12345"

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileServiceAccount(context.Background(), app); err != nil {
		t.Fatalf("reconcileServiceAccount returned error: %v", err)
	}

	sa := &corev1.ServiceAccount{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testSAName, Namespace: testNamespace}, sa); err != nil {
		t.Fatalf("failed to get ServiceAccount: %v", err)
	}

	if len(sa.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(sa.OwnerReferences))
	}
	if sa.OwnerReferences[0].Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, sa.OwnerReferences[0].Name)
	}
}

func TestAnnotateServiceAccountWithIRSA_SetsAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	roleArn := testRoleARN
	if err := r.annotateServiceAccountWithIRSA(context.Background(), app, roleArn); err != nil {
		t.Fatalf("annotateServiceAccountWithIRSA returned error: %v", err)
	}

	sa := &corev1.ServiceAccount{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testSAName, Namespace: testNamespace}, sa); err != nil {
		t.Fatalf("failed to get ServiceAccount: %v", err)
	}

	if sa.Annotations["eks.amazonaws.com/role-arn"] != roleArn {
		t.Fatalf("expected IRSA annotation %q, got %q", roleArn, sa.Annotations["eks.amazonaws.com/role-arn"])
	}
}

func TestAnnotateServiceAccountWithIRSA_SkipsWhenCreateIsFalse(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	falseVal := false
	app := newTestApplication()
	app.Spec.ServiceAccount = &forgev1alpha1.ServiceAccountSpec{Create: &falseVal, Name: testCustomSAName}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	// A user-managed ServiceAccount must not be force-owned or mutated just
	// because IRSA needs an annotation somewhere -- the Role ARN is already
	// surfaced on Application.Status for the user to wire in themselves.
	if err := r.annotateServiceAccountWithIRSA(context.Background(), app, testRoleARN); err != nil {
		t.Fatalf("annotateServiceAccountWithIRSA returned error: %v", err)
	}

	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testCustomSAName, Namespace: testNamespace}, &corev1.ServiceAccount{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no ServiceAccount to be created/touched when Create is false, got err=%v", err)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcileServiceAccount_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.reconcileServiceAccount(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcileServiceAccount, got nil")
	}
}

func TestAnnotateServiceAccountWithIRSA_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.annotateServiceAccountWithIRSA(context.Background(), app, testRoleARN)
	if err == nil {
		t.Fatalf("expected error from annotateServiceAccountWithIRSA, got nil")
	}
}
