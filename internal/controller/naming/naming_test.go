package naming

import (
	"context"
	"strings"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testAppName = "demo-app"
const testDefaultAkamaiToken = "demo-app-akamai-token"
const testNamespace = "default"
const testRolePrefix = "app-irsa"

func newTestApplication() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace},
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
	got := CloudResourceName([]string{testRolePrefix, testNamespace, testAppName}, 64)
	if got != "app-irsa-default-demo-app" {
		t.Fatalf("expected unchanged short name, got %q", got)
	}
}

func TestCloudResourceName_DifferentNamespacesDontCollide(t *testing.T) {
	a := CloudResourceName([]string{testRolePrefix, "team-a", testAppName}, 64)
	b := CloudResourceName([]string{testRolePrefix, "team-b", testAppName}, 64)
	if a == b {
		t.Fatalf("expected different namespaces to produce different names, both got %q", a)
	}
}

func TestCloudResourceName_TruncatesAndHashesWhenTooLong(t *testing.T) {
	longNamespace := "a-namespace-name-that-is-unreasonably-long-for-a-real-cluster"
	longName := "an-equally-long-application-name-nobody-would-actually-use"

	got := CloudResourceName([]string{testRolePrefix, longNamespace, longName}, 64)

	if len(got) > 64 {
		t.Fatalf("expected result within maxLen 64, got %d chars: %q", len(got), got)
	}
	if got == "app-irsa-"+longNamespace+"-"+longName {
		t.Fatalf("expected truncation to actually occur for an oversized input")
	}
}

func TestCloudResourceName_TruncationStaysUniquePerInput(t *testing.T) {
	longNamespace := "a-namespace-name-that-is-unreasonably-long-for-a-real-cluster"

	a := CloudResourceName([]string{testRolePrefix, longNamespace, "app-one-with-a-very-long-name-too"}, 64)
	b := CloudResourceName([]string{testRolePrefix, longNamespace, "app-two-with-a-very-long-name-too"}, 64)

	if a == b {
		t.Fatalf("expected two different oversized inputs to still produce different truncated names, both got %q", a)
	}
}

func TestCloudResourceName_HandlesMaxLenSmallerThanHashSuffix(t *testing.T) {
	// Must not panic even for a maxLen too small to fit any real content --
	// exercises the keep<0 clamp.
	got := CloudResourceName([]string{testRolePrefix, testNamespace, testAppName}, 3)
	if len(got) == 0 {
		t.Fatalf("expected a non-empty result even for a very small maxLen")
	}
}

// --- ApplicationExistsWithUID ---

const (
	testOwnerUID    = types.UID("11111111-1111-1111-1111-111111111111")
	testAdopterNS   = "team-b"
	testOwnerNS     = "team-a"
	testUnrelatedNS = "team-c"
)

func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := forgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add forgev1alpha1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestApplicationExistsWithUID(t *testing.T) {
	owner := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: testOwnerNS, UID: testOwnerUID},
	}
	c := newFakeClient(t, owner)

	t.Run("empty UID is never found", func(t *testing.T) {
		exists, err := ApplicationExistsWithUID(context.Background(), c, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Fatalf("expected empty UID to never be reported as existing")
		}
	})

	t.Run("finds an existing Application by UID regardless of namespace", func(t *testing.T) {
		exists, err := ApplicationExistsWithUID(context.Background(), c, testOwnerUID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists {
			t.Fatalf("expected the owner's UID to be found")
		}
	})

	t.Run("reports false for a UID nothing carries", func(t *testing.T) {
		exists, err := ApplicationExistsWithUID(context.Background(), c, "99999999-9999-9999-9999-999999999999")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Fatalf("expected a UID nothing carries to be reported as not found")
		}
	})
}

// --- CrossNamespaceAdoptionAllowed ---

func namespaceWithGrant(grant string) *corev1.Namespace {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testOwnerNS}}
	if grant != "" {
		ns.Annotations = map[string]string{AllowBucketAdoptionFromAnnotation: grant}
	}
	return ns
}

func TestCrossNamespaceAdoptionAllowed(t *testing.T) {
	tests := []struct {
		name        string
		ownerNS     *corev1.Namespace
		ownerNSName string
		adoptingNS  string
		want        bool
	}{
		{
			name:        "owner namespace has no grant annotation at all",
			ownerNS:     namespaceWithGrant(""),
			ownerNSName: testOwnerNS,
			adoptingNS:  testAdopterNS,
			want:        false,
		},
		{
			name:        "wildcard grant allows any namespace",
			ownerNS:     namespaceWithGrant("*"),
			ownerNSName: testOwnerNS,
			adoptingNS:  testAdopterNS,
			want:        true,
		},
		{
			name:        "exact namespace named in a comma-separated list",
			ownerNS:     namespaceWithGrant(testUnrelatedNS + "," + testAdopterNS),
			ownerNSName: testOwnerNS,
			adoptingNS:  testAdopterNS,
			want:        true,
		},
		{
			name:        "whitespace around list entries is trimmed",
			ownerNS:     namespaceWithGrant(testUnrelatedNS + " , " + testAdopterNS + " "),
			ownerNSName: testOwnerNS,
			adoptingNS:  testAdopterNS,
			want:        true,
		},
		{
			name:        "namespace not named in the list is refused",
			ownerNS:     namespaceWithGrant(testUnrelatedNS),
			ownerNSName: testOwnerNS,
			adoptingNS:  testAdopterNS,
			want:        false,
		},
		{
			name:        "owner namespace object doesn't exist at all",
			ownerNS:     nil,
			ownerNSName: "namespace-does-not-exist",
			adoptingNS:  testAdopterNS,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			if tt.ownerNS != nil {
				objs = append(objs, tt.ownerNS)
			}
			c := newFakeClient(t, objs...)

			got, err := CrossNamespaceAdoptionAllowed(context.Background(), c, tt.ownerNSName, tt.adoptingNS)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

// --- EvaluateBucketAdoption ---

func TestEvaluateBucketAdoption_RefusesWhilePreviousOwnerExists(t *testing.T) {
	owner := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: testOwnerNS, UID: testOwnerUID},
	}
	c := newFakeClient(t, owner)

	err := EvaluateBucketAdoption(context.Background(), c, testOwnerUID, testOwnerNS, testAdopterNS)
	if err == nil {
		t.Fatalf("expected an error while the previous owner still exists")
	}
	if !strings.Contains(err.Error(), "still exists") {
		t.Fatalf("expected the error to explain the previous owner still exists, got: %v", err)
	}
}

func TestEvaluateBucketAdoption_AllowsSameNamespaceOnceOwnerGone(t *testing.T) {
	c := newFakeClient(t)

	if err := EvaluateBucketAdoption(context.Background(), c, testOwnerUID, testOwnerNS, testOwnerNS); err != nil {
		t.Fatalf("expected same-namespace adoption to succeed once the owner is gone, got: %v", err)
	}
}

func TestEvaluateBucketAdoption_RefusesLegacyBucketWithNoRecordedNamespace(t *testing.T) {
	c := newFakeClient(t)

	// ownerNamespace == "" means this bucket was tagged/marked before
	// namespace-scoped ownership shipped -- there's nothing to compare
	// against, so cross-namespace adoption must fail closed rather than
	// silently allow it just because the previous owner happens to be gone.
	err := EvaluateBucketAdoption(context.Background(), c, testOwnerUID, "", testAdopterNS)
	if err == nil {
		t.Fatalf("expected an error for a bucket with no recorded owner namespace")
	}
	if !strings.Contains(err.Error(), "namespace-scoped adoption shipped") {
		t.Fatalf("expected the error to explain the missing namespace record, got: %v", err)
	}
}

func TestEvaluateBucketAdoption_RefusesCrossNamespaceWithoutGrant(t *testing.T) {
	c := newFakeClient(t, namespaceWithGrant(""))

	err := EvaluateBucketAdoption(context.Background(), c, testOwnerUID, testOwnerNS, testAdopterNS)
	if err == nil {
		t.Fatalf("expected cross-namespace adoption to be refused without a grant")
	}
	if !strings.Contains(err.Error(), AllowBucketAdoptionFromAnnotation) {
		t.Fatalf("expected the error to name the missing annotation, got: %v", err)
	}
}

func TestEvaluateBucketAdoption_AllowsCrossNamespaceWithGrant(t *testing.T) {
	c := newFakeClient(t, namespaceWithGrant(testAdopterNS))

	if err := EvaluateBucketAdoption(context.Background(), c, testOwnerUID, testOwnerNS, testAdopterNS); err != nil {
		t.Fatalf("expected cross-namespace adoption to succeed with an explicit grant, got: %v", err)
	}
}

func TestEvaluateBucketAdoption_AllowsCrossNamespaceWithWildcardGrant(t *testing.T) {
	c := newFakeClient(t, namespaceWithGrant("*"))

	if err := EvaluateBucketAdoption(context.Background(), c, testOwnerUID, testOwnerNS, testAdopterNS); err != nil {
		t.Fatalf("expected cross-namespace adoption to succeed with a wildcard grant, got: %v", err)
	}
}
