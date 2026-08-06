package akamaiobjstr

import (
	"context"
	"errors"
	"net/http"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
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
			if opts.Label != "demo-bucket" || opts.Cluster != "us-east-1" {
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
				{Label: "demo-app-key", AccessKey: "existing-access-key"},
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
	if result.AccessKey != "existing-access-key" {
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
			if opts.Label != "demo-app-key" {
				t.Errorf("expected key label demo-app-key, got %q", opts.Label)
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

	if got := m.resolveEndpoint(); got != "custom.endpoint.example.com" {
		t.Fatalf("expected configured endpoint, got %q", got)
	}
}

func TestResolveEndpoint_DefaultsToRegionBasedEndpoint(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = ""
	m.region = "us-east-1"

	if got := m.resolveEndpoint(); got != "us-east-1.linodeobjects.com" {
		t.Fatalf("expected default region-based endpoint, got %q", got)
	}
}

// --- ReconcileBucket ---

func TestReconcileBucket_HappyPath(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: "demo-app-key", AccessKey: "existing-access-key"}}, nil
		},
	})

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.AccessKey != "existing-access-key" {
		t.Errorf("expected access key to be returned, got %q", result.AccessKey)
	}
	if result.Endpoint != "us-east-1.linodeobjects.com" {
		t.Errorf("expected default endpoint, got %q", result.Endpoint)
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
