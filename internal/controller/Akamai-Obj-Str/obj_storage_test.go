package akamaiobjstr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/linodego"
)

func notFoundErr() error {
	return &linodego.Error{Code: http.StatusNotFound}
}

// --- validateStorageSpec ---

func TestValidateStorageSpec(t *testing.T) {
	tests := []struct {
		name    string
		storage *forgev1alpha1.StorageSpec
		bucket  string
		region  string
		wantErr bool
	}{
		{name: "nil storage spec", storage: nil, bucket: "b", region: "r", wantErr: true},
		{name: "empty bucket", storage: &forgev1alpha1.StorageSpec{}, bucket: "", region: "r", wantErr: true},
		{name: "empty region", storage: &forgev1alpha1.StorageSpec{}, bucket: "b", region: "", wantErr: true},
		{name: "valid spec", storage: &forgev1alpha1.StorageSpec{}, bucket: "b", region: "r", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestManager(nil)
			m.storage = tt.storage
			m.bucket = tt.bucket
			m.region = tt.region

			err := m.validateStorageSpec()
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

// --- ensureBucketExists ---

func TestEnsureBucketExists_ReturnsExistingBucket(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		createObjectStorageBucketFunc: func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
			createCalled = true
			return &linodego.ObjectStorageBucket{}, nil
		},
	})

	bucket, err := m.ensureBucketExists(context.Background())
	if err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if bucket == nil {
		t.Fatalf("expected bucket to be returned")
	}
	if createCalled {
		t.Fatalf("expected CreateObjectStorageBucket not to be called when bucket exists")
	}
}

func TestEnsureBucketExists_CreatesBucketWhenNotFound(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, notFoundErr()
		},
		createObjectStorageBucketFunc: func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
			createCalled = true
			if opts.Label != testBucket || opts.Region != testRegion {
				t.Errorf("unexpected create options: %#v", opts)
			}
			return &linodego.ObjectStorageBucket{Label: opts.Label}, nil
		},
	})

	bucket, err := m.ensureBucketExists(context.Background())
	if err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if bucket == nil {
		t.Fatalf("expected bucket to be returned")
	}
	if !createCalled {
		t.Fatalf("expected CreateObjectStorageBucket to be called when bucket is not found")
	}
}

func TestEnsureBucketExists_PropagatesGetError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, errors.New("query failed")
		},
	})

	_, err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureBucketExists, got nil")
	}
}

func TestEnsureBucketExists_PropagatesCreateError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, notFoundErr()
		},
		createObjectStorageBucketFunc: func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
			return nil, errors.New("create failed")
		},
	})

	_, err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureBucketExists, got nil")
	}
}

// --- ensureAccessKey ---

func TestEnsureAccessKey_ReusesExistingKey(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{
				{Label: testAccessKeyLabel, AccessKey: testExistingAccessKey},
			}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			createCalled = true
			return &linodego.ObjectStorageKey{}, nil
		},
	})

	result, err := m.ensureAccessKey(context.Background())
	if err != nil {
		t.Fatalf("ensureAccessKey returned error: %v", err)
	}
	if result.AccessKey != testExistingAccessKey {
		t.Errorf("expected existing access key to be reused, got %q", result.AccessKey)
	}
	if result.SecretKey != "" {
		t.Errorf("expected empty secret key when reusing existing key, got %q", result.SecretKey)
	}
	if createCalled {
		t.Fatalf("expected CreateObjectStorageKey not to be called when key already exists")
	}
}

func TestEnsureAccessKey_CreatesNewKeyWhenNoneExists(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			if opts.Label != testAccessKeyLabel {
				t.Errorf("expected key label demo-app-key, got %q", opts.Label)
			}
			// The whole reason BucketAccess is used instead of an unscoped
			// key: without a correct BucketName, the generated key would
			// grant access beyond this Application's own bucket.
			if opts.BucketAccess == nil || len(*opts.BucketAccess) != 1 {
				t.Fatalf("expected exactly one BucketAccess entry, got %#v", opts.BucketAccess)
			}
			access := (*opts.BucketAccess)[0]
			if access.BucketName != testBucket {
				t.Errorf("expected key scoped to bucket %q, got %q", testBucket, access.BucketName)
			}
			if access.Permissions != "read_write" {
				t.Errorf("expected read_write permissions, got %q", access.Permissions)
			}
			return &linodego.ObjectStorageKey{AccessKey: "new-access-key", SecretKey: "new-secret-key"}, nil
		},
	})

	result, err := m.ensureAccessKey(context.Background())
	if err != nil {
		t.Fatalf("ensureAccessKey returned error: %v", err)
	}
	if result.AccessKey != "new-access-key" || result.SecretKey != "new-secret-key" {
		t.Errorf("expected new key credentials to be returned, got %#v", result)
	}
}

func TestEnsureAccessKey_PropagatesListError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return nil, errors.New("list failed")
		},
	})

	_, err := m.ensureAccessKey(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureAccessKey, got nil")
	}
}

func TestEnsureAccessKey_PropagatesCreateError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			return nil, errors.New("create failed")
		},
	})

	_, err := m.ensureAccessKey(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureAccessKey, got nil")
	}
}

// --- resolveEndpoint ---

func TestResolveEndpoint_UsesConfiguredEndpoint(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = "custom.endpoint.example.com"

	bucket := &linodego.ObjectStorageBucket{Hostname: "bucket.us-east-1.linodeobjects.com"}
	if got := m.resolveEndpoint(bucket); got != "custom.endpoint.example.com" {
		t.Fatalf("expected configured endpoint, got %q", got)
	}
}

func TestResolveEndpoint_PrefersBucketHostname(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = ""
	m.region = testRegion

	bucket := &linodego.ObjectStorageBucket{Hostname: "bucket-label.us-east-1.linodeobjects.com"}
	if got := m.resolveEndpoint(bucket); got != "bucket-label.us-east-1.linodeobjects.com" {
		t.Fatalf("expected bucket hostname, got %q", got)
	}
}

func TestResolveEndpoint_DefaultsToRegionBasedEndpoint(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = ""
	m.region = testRegion

	// No bucket at all, and a bucket with no Hostname, both fall back the
	// same way.
	if got := m.resolveEndpoint(nil); got != testDefaultEndpoint {
		t.Fatalf("expected default region-based endpoint, got %q", got)
	}
	if got := m.resolveEndpoint(&linodego.ObjectStorageBucket{}); got != testDefaultEndpoint {
		t.Fatalf("expected default region-based endpoint, got %q", got)
	}
}

// --- ReconcileBucket ---

func TestReconcileBucket_HappyPath(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testAppUID)))}, nil
		},
	})

	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, AccessKey: testExistingAccessKey}}, nil
		},
	})

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.AccessKey != testExistingAccessKey {
		t.Errorf("expected access key to be returned, got %q", result.AccessKey)
	}
	if result.Endpoint != testDefaultEndpoint {
		t.Errorf("expected default endpoint, got %q", result.Endpoint)
	}
}

// --- claimOrVerifyOwnership / claimOwnership ---

func TestClaimOrVerifyOwnership_ClaimsWhenMarkerMissing(t *testing.T) {
	claimed := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return nil, &s3types.NoSuchKey{}
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			claimed = true
			body, _ := io.ReadAll(params.Body)
			if string(body) != string(testAppUID) {
				t.Errorf("expected marker body to be the app UID, got %q", body)
			}
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	m := newTestManager(nil)

	if err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk"); err != nil {
		t.Fatalf("claimOrVerifyOwnership returned error: %v", err)
	}
	if !claimed {
		t.Fatalf("expected a missing marker to be claimed")
	}
}

func TestClaimOrVerifyOwnership_ProceedsWhenMarkerMatches(t *testing.T) {
	putCalled := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testAppUID)))}, nil
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			putCalled = true
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	m := newTestManager(nil)

	if err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk"); err != nil {
		t.Fatalf("claimOrVerifyOwnership returned error: %v", err)
	}
	if putCalled {
		t.Fatalf("expected no write when the marker already matches")
	}
}

func TestClaimOrVerifyOwnership_ReturnsNotOwnedWhenMarkerMismatched(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
	})

	m := newTestManager(nil)

	err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk")
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestClaimOrVerifyOwnership_ReturnsNotOwnedOnGenericGetError(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			// A genuine failure (permission denied, network error, ...) must
			// NOT be treated as claimable -- only a typed NoSuchKey is.
			return nil, errors.New("access denied")
		},
	})

	m := newTestManager(nil)

	err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk")
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestClaimOwnership_PropagatesPutError(t *testing.T) {
	m := newTestManager(nil)

	err := m.claimOwnership(context.Background(), &mockS3ObjectClient{
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			return nil, errors.New("write denied")
		},
	})
	if err == nil {
		t.Fatalf("expected error from claimOwnership, got nil")
	}
}

func TestReconcileBucket_ReturnsErrorOnInvalidSpec(t *testing.T) {
	m := newTestManager(nil)
	m.storage = nil

	_, err := m.ReconcileBucket(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileBucket with nil storage spec, got nil")
	}
}

func TestReconcileBucket_ShortCircuitsOnBucketError(t *testing.T) {
	keyListCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, errors.New("bucket query failed")
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			keyListCalled = true
			return nil, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileBucket, got nil")
	}
	if keyListCalled {
		t.Fatalf("expected access key reconciliation to be skipped after bucket error")
	}
}

func TestReconcileBucket_ShortCircuitsOnNotOwnedBucket(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
	})

	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: testAccessKeyLabel, AccessKey: testExistingAccessKey}}, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned from ReconcileBucket, got %v", err)
	}
}
