package naming

import (
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testAppName = "demo-app"
const testDefaultAkamaiToken = "demo-app-akamai-token"

func newTestApplication() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: "default"},
	}
}

func TestNames(t *testing.T) {
	app := newTestApplication()

	tests := []struct {
		name     string
		fn       func(*forgev1alpha1.Application) string
		expected string
	}{
		{name: "Service", fn: Service, expected: testAppName},
		{name: "Deployment", fn: Deployment, expected: "demo-app-deployment"},
		{name: "Ingress", fn: Ingress, expected: testAppName},
		{name: "HPA", fn: HPA, expected: "demo-app-hpa"},
		{name: "PDB", fn: PDB, expected: "demo-app-pdb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(app); got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestStorageSecret(t *testing.T) {
	t.Run("defaults when spec.storage is nil", func(t *testing.T) {
		app := newTestApplication()
		if got := StorageSecret(app); got != "demo-app-storage" {
			t.Fatalf("expected demo-app-storage, got %q", got)
		}
	})

	t.Run("defaults when secretName is unset", func(t *testing.T) {
		app := newTestApplication()
		app.Spec.Storage = &forgev1alpha1.StorageSpec{}
		if got := StorageSecret(app); got != "demo-app-storage" {
			t.Fatalf("expected demo-app-storage, got %q", got)
		}
	})

	t.Run("honors an explicit secretName", func(t *testing.T) {
		app := newTestApplication()
		app.Spec.Storage = &forgev1alpha1.StorageSpec{SecretName: "custom-storage"}
		if got := StorageSecret(app); got != "custom-storage" {
			t.Fatalf("expected custom-storage, got %q", got)
		}
	})
}

func TestAkamaiTokenSecret(t *testing.T) {
	t.Run("defaults when spec.storage is nil", func(t *testing.T) {
		app := newTestApplication()
		if got := AkamaiTokenSecret(app); got != testDefaultAkamaiToken {
			t.Fatalf("expected demo-app-akamai-token, got %q", got)
		}
	})

	t.Run("defaults when spec.storage.akamai is nil", func(t *testing.T) {
		app := newTestApplication()
		app.Spec.Storage = &forgev1alpha1.StorageSpec{}
		if got := AkamaiTokenSecret(app); got != testDefaultAkamaiToken {
			t.Fatalf("expected demo-app-akamai-token, got %q", got)
		}
	})

	t.Run("defaults when accessKeySecretRef is unset", func(t *testing.T) {
		app := newTestApplication()
		app.Spec.Storage = &forgev1alpha1.StorageSpec{Akamai: &forgev1alpha1.AkamaiStorageSpec{}}
		if got := AkamaiTokenSecret(app); got != testDefaultAkamaiToken {
			t.Fatalf("expected demo-app-akamai-token, got %q", got)
		}
	})

	t.Run("honors an explicit accessKeySecretRef, distinct from StorageSecret's default", func(t *testing.T) {
		app := newTestApplication()
		app.Spec.Storage = &forgev1alpha1.StorageSpec{
			Akamai: &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "custom-token"},
		}
		if got := AkamaiTokenSecret(app); got != "custom-token" {
			t.Fatalf("expected custom-token, got %q", got)
		}
		if StorageSecret(app) == AkamaiTokenSecret(app) {
			t.Fatalf("StorageSecret and AkamaiTokenSecret must never default to the same name")
		}
	})
}
