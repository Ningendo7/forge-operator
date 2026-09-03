package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testNamespace           = "default"
	testAPIKey              = "API_KEY"
	testCustomSecretName    = "custom-secret"
	testCustomConfigMapName = "custom-config"
	testCustomSAName        = "custom-sa"
	testConditionFalse      = "False"
	testRoleARN             = "arn:aws:iam::123456789012:role/demo-role"
	testAccessKey           = "access-123"
	testDataKey             = "foo"
	testDataValue           = "bar"
	testAppName             = "demo-app"
	testImage               = "nginx:latest"
	testImage127            = "nginx:1.27"
	testHPAName             = "demo-app-hpa"
	testDeploymentName      = "demo-app-deployment"
	testPDBName             = "demo-app-pdb"
	testAPIKeyValue         = "abc123"
	testSecretAppName       = "demo-app-secret"
	testOldName             = "old-name"
	testNewName             = "new-name"
	testStorageSecretName   = "demo-app-storage"
	testExampleHost         = "example.com"
	testConfigMapName       = "demo-app-config"
	testSAName              = "demo-app-sa"
	testMissingCredsSecret  = "missing-creds"
	testWestRegion          = "us-west-2"
	testAkamaiSecretKey     = "secret-456"
	testAkamaiEndpoint      = "us-east-1.linodeobjects.com"
	testSharedCredsSecret   = "shared-creds"
	testBucket              = "demo-bucket"
)

func newTestApplication() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAppName,
			Namespace: testNamespace,
		},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: testImage,
		},
	}
}

type failingPatchClient struct {
	client.Client
}

func (c *failingPatchClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	return fmt.Errorf("patch failed")
}

type mockS3StorageManager struct {
	reconcileBucketFunc func(ctx context.Context) (*s3storage.StorageResult, error)
}

func (m *mockS3StorageManager) ReconcileBucket(ctx context.Context) (*s3storage.StorageResult, error) {
	return m.reconcileBucketFunc(ctx)
}

type mockAkamaiStorageManager struct {
	reconcileBucketFunc func(ctx context.Context) (*akamaiobjstr.StorageResult, error)
}

func (m *mockAkamaiStorageManager) ReconcileBucket(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
	return m.reconcileBucketFunc(ctx)
}

// fakeEvent is one call recorded by fakeEventRecorder.
type fakeEvent struct {
	eventtype string
	reason    string
	action    string
	note      string
}

// fakeEventRecorder is a minimal events.EventRecorder test double -- there
// wasn't one before this, so nothing previously verified an Event actually
// gets recorded (as opposed to just checking the code path that would emit
// one doesn't error).
type fakeEventRecorder struct {
	events []fakeEvent
}

func (f *fakeEventRecorder) Eventf(regarding, related runtime.Object, eventtype, reason, action, note string, args ...any) {
	f.events = append(f.events, fakeEvent{
		eventtype: eventtype,
		reason:    reason,
		action:    action,
		note:      fmt.Sprintf(note, args...),
	})
}
