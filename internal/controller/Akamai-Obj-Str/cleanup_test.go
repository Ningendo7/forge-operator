package akamaiobjstr

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3sdktypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/linodego"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

// seedRecoverableSecret writes an output Secret with a real secret_key, so
// ensureAccessKey's reuse path (called both directly and via
// verifyOwnershipAndEmptyBucket) can recover it instead of deleting and
// recreating the key -- the state a genuinely established Application would
// already be in.
func seedRecoverableSecret(t *testing.T, m *Manager) {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testStorageSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretKeyDataKey: []byte("recovered-secret")},
	}
	if err := m.k8sClient.Create(context.Background(), secret); err != nil {
		t.Fatalf("failed to seed storage Secret: %v", err)
	}
}

// withMatchingMarker sets up an s3ObjectAPI whose marker object matches
// this test's Application UID, so verifyOwnershipAndEmptyBucket's ownership
// check passes and it proceeds to (finding nothing to delete in) an empty
// bucket.
func withMatchingMarker(t *testing.T) {
	t.Helper()
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testAppUID)))}, nil
		},
	})
}

// --- findAccessKeyIDByLabel ---

func TestFindAccessKeyIDByLabel_ReturnsMatchingID(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{
				{Label: "other-key", ID: 1},
				{Label: testAccessKeyLabel, ID: 42},
			}, nil
		},
	})

	id, err := m.findAccessKeyIDByLabel(context.Background(), testAccessKeyLabel)
	if err != nil {
		t.Fatalf("findAccessKeyIDByLabel returned error: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected id 42, got %d", id)
	}
}

func TestFindAccessKeyIDByLabel_ReturnsZeroWhenNotFound(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
	})

	id, err := m.findAccessKeyIDByLabel(context.Background(), "missing-key")
	if err != nil {
		t.Fatalf("findAccessKeyIDByLabel returned error: %v", err)
	}
	if id != 0 {
		t.Fatalf("expected id 0 when key not found, got %d", id)
	}
}

func TestFindAccessKeyIDByLabel_PropagatesListError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return nil, errors.New("list failed")
		},
	})

	_, err := m.findAccessKeyIDByLabel(context.Background(), testAccessKeyLabel)
	if err == nil {
		t.Fatalf("expected error from findAccessKeyIDByLabel, got nil")
	}
}

// --- deleteApplicationAccessKey ---

func TestDeleteApplicationAccessKey_DeletesWhenFound(t *testing.T) {
	deleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, ID: 42}}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deleteCalled = true
			if keyID != 42 {
				t.Errorf("expected keyID 42, got %d", keyID)
			}
			return nil
		},
	})

	if err := m.deleteApplicationAccessKey(context.Background()); err != nil {
		t.Fatalf("deleteApplicationAccessKey returned error: %v", err)
	}
	if !deleteCalled {
		t.Fatalf("expected DeleteObjectStorageKey to be called")
	}
}

func TestDeleteApplicationAccessKey_NoOpWhenKeyMissing(t *testing.T) {
	deleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deleteCalled = true
			return nil
		},
	})

	if err := m.deleteApplicationAccessKey(context.Background()); err != nil {
		t.Fatalf("expected nil error when key already gone, got %v", err)
	}
	if deleteCalled {
		t.Fatalf("expected DeleteObjectStorageKey not to be called when key does not exist")
	}
}

func TestDeleteApplicationAccessKey_PropagatesDeleteError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, ID: 42}}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			return errors.New("delete failed")
		},
	})

	if err := m.deleteApplicationAccessKey(context.Background()); err == nil {
		t.Fatalf("expected error from deleteApplicationAccessKey, got nil")
	}
}

// --- deleteStorageBucket ---

func TestDeleteStorageBucket_Success(t *testing.T) {
	called := false
	m := newTestManager(&mockAkamaiClient{
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			called = true
			return nil
		},
	})

	if err := m.deleteStorageBucket(context.Background()); err != nil {
		t.Fatalf("deleteStorageBucket returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected DeleteObjectStorageBucket to be called")
	}
}

func TestDeleteStorageBucket_IgnoresNotFound(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			return notFoundErr()
		},
	})

	if err := m.deleteStorageBucket(context.Background()); err != nil {
		t.Fatalf("expected nil error when bucket already deleted, got %v", err)
	}
}

func TestDeleteStorageBucket_PropagatesOtherErrors(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			return errors.New("delete failed")
		},
	})

	if err := m.deleteStorageBucket(context.Background()); err == nil {
		t.Fatalf("expected error from deleteStorageBucket, got nil")
	}
}

// --- DeleteBucket ---

func TestDeleteBucket_NoOpWhenBucketNameEmpty(t *testing.T) {
	deleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			deleteCalled = true
			return nil
		},
	})
	m.bucket = ""

	accessKeyErr, err := m.DeleteBucket(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when bucket name is empty, got %v", err)
	}
	if accessKeyErr != nil {
		t.Fatalf("expected nil accessKeyErr when bucket name is empty, got %v", accessKeyErr)
	}
	if deleteCalled {
		t.Fatalf("expected DeleteObjectStorageBucket not to be called when bucket name is empty")
	}
}

func TestDeleteBucket_HappyPath(t *testing.T) {
	withMatchingMarker(t)
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, ID: 42}}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			return nil
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			return nil
		},
	})
	seedRecoverableSecret(t, m)

	accessKeyErr, err := m.DeleteBucket(context.Background())
	if err != nil {
		t.Fatalf("DeleteBucket returned error: %v", err)
	}
	if accessKeyErr != nil {
		t.Fatalf("expected nil accessKeyErr on happy path, got %v", accessKeyErr)
	}
}

func TestDeleteBucket_StillDeletesBucketWhenAccessKeyCleanupFails(t *testing.T) {
	withMatchingMarker(t)
	bucketDeleteCalled := false
	listCalls := 0
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			// First call is verifyOwnershipAndEmptyBucket's own
			// ensureAccessKey, which must succeed (it recovers the seeded
			// secret below) so verification/emptying can complete. Second
			// call is deleteApplicationAccessKey's own lookup -- failing
			// that is the actual access-key cleanup failure this test
			// exercises.
			listCalls++
			if listCalls == 1 {
				return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, ID: 42}}, nil
			}
			return nil, errors.New("list failed")
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			bucketDeleteCalled = true
			return nil
		},
	})
	seedRecoverableSecret(t, m)

	accessKeyErr, err := m.DeleteBucket(context.Background())
	if err != nil {
		t.Fatalf("expected DeleteBucket to succeed despite access key cleanup failure, got %v", err)
	}
	if !bucketDeleteCalled {
		t.Fatalf("expected bucket deletion to proceed even when access key cleanup fails")
	}
	// The failure isn't swallowed entirely -- it comes back separately so the
	// caller can surface it (e.g. as an Event) without it blocking cleanup.
	if accessKeyErr == nil {
		t.Fatalf("expected the access key cleanup failure to be surfaced via accessKeyErr")
	}
}

func TestDeleteBucket_PropagatesBucketDeletionError(t *testing.T) {
	withMatchingMarker(t)
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			return &linodego.ObjectStorageKey{AccessKey: testNewAccessKey, SecretKey: testNewSecretKey}, nil
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			return errors.New("delete bucket failed")
		},
	})

	if _, err := m.DeleteBucket(context.Background()); err == nil {
		t.Fatalf("expected error from DeleteBucket, got nil")
	}
}

// --- emptyBucket ---

func TestEmptyBucket_DeletesObjects(t *testing.T) {
	var deletedKeys []string
	m := newTestManager(nil)
	client := &mockS3ObjectClient{
		listObjectsV2Func: func(ctx context.Context, params *s3sdk.ListObjectsV2Input, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectsV2Output, error) {
			return &s3sdk.ListObjectsV2Output{
				Contents: []s3sdktypes.Object{
					{Key: awsString(".forge-operator-owner")},
					{Key: awsString("some-object.txt")},
				},
			}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			for _, obj := range params.Delete.Objects {
				deletedKeys = append(deletedKeys, *obj.Key)
			}
			return &s3sdk.DeleteObjectsOutput{}, nil
		},
	}

	if err := m.emptyBucket(context.Background(), client); err != nil {
		t.Fatalf("emptyBucket returned error: %v", err)
	}
	if len(deletedKeys) != 2 {
		t.Fatalf("expected 2 objects deleted, got %d: %v", len(deletedKeys), deletedKeys)
	}
}

func TestEmptyBucket_SkipsDeleteCallWhenPageEmpty(t *testing.T) {
	m := newTestManager(nil)
	deleteCalled := false
	client := &mockS3ObjectClient{
		listObjectsV2Func: func(ctx context.Context, params *s3sdk.ListObjectsV2Input, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectsV2Output, error) {
			return &s3sdk.ListObjectsV2Output{}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			deleteCalled = true
			return &s3sdk.DeleteObjectsOutput{}, nil
		},
	}

	if err := m.emptyBucket(context.Background(), client); err != nil {
		t.Fatalf("emptyBucket returned error: %v", err)
	}
	if deleteCalled {
		t.Fatalf("expected DeleteObjects not to be called when the bucket is already empty")
	}
}

func TestEmptyBucket_PropagatesListError(t *testing.T) {
	m := newTestManager(nil)
	client := &mockS3ObjectClient{
		listObjectsV2Func: func(ctx context.Context, params *s3sdk.ListObjectsV2Input, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectsV2Output, error) {
			return nil, errors.New("list failed")
		},
	}

	if err := m.emptyBucket(context.Background(), client); err == nil {
		t.Fatalf("expected error from emptyBucket, got nil")
	}
}

func TestEmptyBucket_PropagatesPerObjectDeleteErrors(t *testing.T) {
	m := newTestManager(nil)
	client := &mockS3ObjectClient{
		listObjectsV2Func: func(ctx context.Context, params *s3sdk.ListObjectsV2Input, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectsV2Output, error) {
			return &s3sdk.ListObjectsV2Output{Contents: []s3sdktypes.Object{{Key: awsString("stuck.txt")}}}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			return &s3sdk.DeleteObjectsOutput{
				Errors: []s3sdktypes.Error{{Key: awsString("stuck.txt"), Message: awsString("access denied")}},
			}, nil
		},
	}

	if err := m.emptyBucket(context.Background(), client); err == nil {
		t.Fatalf("expected error when a per-object delete fails, got nil")
	}
}

func awsString(s string) *string { return &s }

// --- verifyOwnershipAndEmptyBucket / DeleteBucket ownership gating ---

func TestDeleteBucket_RefusesWhenMarkerMismatched(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
	})
	deleteBucketCalled := false
	deletedKeyID := 0
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, ID: 42}}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deletedKeyID = keyID
			return nil
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			deleteBucketCalled = true
			return nil
		},
	})
	seedRecoverableSecret(t, m)
	// Deliberately different from claimOrVerifyOwnership's create-time
	// behavior: adopt-bucket must not unlock deletion of a marker that, as
	// far as this check can tell, still names a different Application --
	// see the identical reasoning in the s3 package's own equivalent test.
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	_, err := m.DeleteBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned even with the adopt annotation set, got %v", err)
	}
	if deleteBucketCalled {
		t.Fatalf("expected DeleteObjectStorageBucket not to be called when ownership can't be confirmed")
	}
	// The access key ensureAccessKey obtained just to run this check is now
	// useless (this Application will never be allowed to touch this
	// bucket) -- it must not be left behind.
	if deletedKeyID != 42 {
		t.Fatalf("expected the access key ensured for verification to be cleaned up after ownership failed, got deletedKeyID=%d", deletedKeyID)
	}
}

func TestDeleteBucket_RefusesWhenMarkerMissingAndNoProvenance(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return nil, &s3sdktypes.NoSuchKey{}
		},
	})
	deletedKeyID := 0
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, ID: 42}}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deletedKeyID = keyID
			return nil
		},
	})
	seedRecoverableSecret(t, m)

	_, err := m.DeleteBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned when marker missing and no provenance, got %v", err)
	}
	if deletedKeyID != 42 {
		t.Fatalf("expected the access key ensured for verification to be cleaned up after ownership failed, got deletedKeyID=%d", deletedKeyID)
	}
}

func TestDeleteBucket_SkipsVerificationWhenBucketAlreadyGone(t *testing.T) {
	deleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, notFoundErr()
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			deleteCalled = true
			return notFoundErr()
		},
	})

	_, err := m.DeleteBucket(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when bucket is already gone, got %v", err)
	}
	if !deleteCalled {
		t.Fatalf("expected DeleteObjectStorageBucket to still be called (and tolerate NotFound) even though verification was skipped")
	}
}
