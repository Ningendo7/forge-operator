package akamaiobjstr

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/linode/linodego"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace         = "default"
	testBucket            = "demo-bucket"
	testRegion            = "us-east-1"
	testAccessKeyLabel    = "default-demo-app-key"
	testExistingAccessKey = "existing-access-key"
	testDefaultEndpoint   = "us-east-1.linodeobjects.com"
	testAppUID            = types.UID("11111111-1111-1111-1111-111111111111")
	testOtherUID          = types.UID("22222222-2222-2222-2222-222222222222")
)

func newTestApp() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: testNamespace, UID: testAppUID},
	}
}

// mockS3ObjectClient is the fake s3ObjectAPI standing in for a real
// S3-compatible client in ownership tests.
type mockS3ObjectClient struct {
	getObjectFunc func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error)
	putObjectFunc func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error)
}

func (m *mockS3ObjectClient) GetObject(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
	return m.getObjectFunc(ctx, params, optFns...)
}

func (m *mockS3ObjectClient) PutObject(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
	return m.putObjectFunc(ctx, params, optFns...)
}

// withS3ObjectClient swaps newS3ObjectClient for the duration of a test so
// claimOrVerifyOwnership talks to a fake client instead of attempting a
// real network call.
func withS3ObjectClient(t *testing.T, client s3ObjectAPI) {
	t.Helper()
	original := newS3ObjectClient
	newS3ObjectClient = func(region, clusterEndpoint, accessKey, secretKey string) s3ObjectAPI {
		return client
	}
	t.Cleanup(func() { newS3ObjectClient = original })
}

// newTestManager builds a Manager with a real fake k8s client (seeded with
// the Application) backing m.k8sClient, needed since recordBucketCreated
// durably writes to Application.Status via that client -- not just the
// mocked AKAMAIAPI/s3ObjectAPI.
func newTestManager(akamaiClient AKAMAIAPI) *Manager {
	app := newTestApp()

	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()

	return &Manager{
		k8sClient:    fakeClient,
		akamaiClient: akamaiClient,
		app:          app,
		storage:      &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testRegion},
		bucket:       testBucket,
		region:       testRegion,
	}
}

type mockAkamaiClient struct {
	listObjectStorageBucketsFunc  func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageBucket, error)
	getObjectStorageBucketFunc    func(ctx context.Context, clusterID string, bucket string) (*linodego.ObjectStorageBucket, error)
	createObjectStorageBucketFunc func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error)
	deleteObjectStorageBucketFunc func(ctx context.Context, clusterID string, bucket string) error

	listObjectStorageKeysFunc  func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error)
	createObjectStorageKeyFunc func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error)
	deleteObjectStorageKeyFunc func(ctx context.Context, keyID int) error
}

func (m *mockAkamaiClient) ListObjectStorageBuckets(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageBucket, error) {
	return m.listObjectStorageBucketsFunc(ctx, opts)
}

func (m *mockAkamaiClient) GetObjectStorageBucket(ctx context.Context, clusterID string, bucket string) (*linodego.ObjectStorageBucket, error) {
	return m.getObjectStorageBucketFunc(ctx, clusterID, bucket)
}

func (m *mockAkamaiClient) CreateObjectStorageBucket(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
	return m.createObjectStorageBucketFunc(ctx, opts)
}

func (m *mockAkamaiClient) DeleteObjectStorageBucket(ctx context.Context, clusterID string, bucket string) error {
	return m.deleteObjectStorageBucketFunc(ctx, clusterID, bucket)
}

func (m *mockAkamaiClient) ListObjectStorageKeys(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
	return m.listObjectStorageKeysFunc(ctx, opts)
}

func (m *mockAkamaiClient) CreateObjectStorageKey(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
	return m.createObjectStorageKeyFunc(ctx, opts)
}

func (m *mockAkamaiClient) DeleteObjectStorageKey(ctx context.Context, keyID int) error {
	return m.deleteObjectStorageKeyFunc(ctx, keyID)
}
