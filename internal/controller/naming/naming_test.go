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

func TestCloudResourceName_ShortNameUnchanged(t *testing.T) {
	got := CloudResourceName([]string{"app-irsa", "default", "demo-app"}, 64)
	if got != "app-irsa-default-demo-app" {
		t.Fatalf("expected unchanged short name, got %q", got)
	}
}

func TestCloudResourceName_DifferentNamespacesDontCollide(t *testing.T) {
	a := CloudResourceName([]string{"app-irsa", "team-a", "demo-app"}, 64)
	b := CloudResourceName([]string{"app-irsa", "team-b", "demo-app"}, 64)
	if a == b {
		t.Fatalf("expected different namespaces to produce different names, both got %q", a)
	}
}

func TestCloudResourceName_TruncatesAndHashesWhenTooLong(t *testing.T) {
	longNamespace := "a-namespace-name-that-is-unreasonably-long-for-a-real-cluster"
	longName := "an-equally-long-application-name-nobody-would-actually-use"

	got := CloudResourceName([]string{"app-irsa", longNamespace, longName}, 64)

	if len(got) > 64 {
		t.Fatalf("expected result within maxLen 64, got %d chars: %q", len(got), got)
	}
	if got == "app-irsa-"+longNamespace+"-"+longName {
		t.Fatalf("expected truncation to actually occur for an oversized input")
	}
}

func TestCloudResourceName_TruncationStaysUniquePerInput(t *testing.T) {
	longNamespace := "a-namespace-name-that-is-unreasonably-long-for-a-real-cluster"

	a := CloudResourceName([]string{"app-irsa", longNamespace, "app-one-with-a-very-long-name-too"}, 64)
	b := CloudResourceName([]string{"app-irsa", longNamespace, "app-two-with-a-very-long-name-too"}, 64)

	if a == b {
		t.Fatalf("expected two different oversized inputs to still produce different truncated names, both got %q", a)
	}
}

func TestCloudResourceName_HandlesMaxLenSmallerThanHashSuffix(t *testing.T) {
	// Must not panic even for a maxLen too small to fit any real content --
	// exercises the keep<0 clamp.
	got := CloudResourceName([]string{"app-irsa", "default", "demo-app"}, 3)
	if len(got) == 0 {
		t.Fatalf("expected a non-empty result even for a very small maxLen")
	}
}
