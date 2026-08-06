package akamaiobjstr

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/linode/linodego"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestApp() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
	}
}

func newTestManager(akamaiClient AKAMAIAPI) *Manager {
	return &Manager{
		akamaiClient: akamaiClient,
		app:          newTestApp(),
		storage:      &forgev1alpha1.StorageSpec{Bucket: "demo-bucket", Region: "us-east-1"},
		bucket:       "demo-bucket",
		region:       "us-east-1",
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
