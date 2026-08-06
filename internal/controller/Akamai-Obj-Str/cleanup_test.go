package akamaiobjstr

import (
	"context"
	"errors"
	"testing"

	"github.com/linode/linodego"
)

// --- findAccessKeyIDByLabel ---

func TestFindAccessKeyIDByLabel_ReturnsMatchingID(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{
				{Label: "other-key", ID: 1},
				{Label: "demo-app-key", ID: 42},
			}, nil
		},
	})

	id, err := m.findAccessKeyIDByLabel(context.Background(), "demo-app-key")
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

	_, err := m.findAccessKeyIDByLabel(context.Background(), "demo-app-key")
	if err == nil {
		t.Fatalf("expected error from findAccessKeyIDByLabel, got nil")
	}
}

// --- deleteApplicationAccessKey ---

func TestDeleteApplicationAccessKey_DeletesWhenFound(t *testing.T) {
	deleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: "demo-app-key", ID: 42}}, nil
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
			return []linodego.ObjectStorageKey{{Label: "demo-app-key", ID: 42}}, nil
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

	if err := m.DeleteBucket(context.Background()); err != nil {
		t.Fatalf("expected nil error when bucket name is empty, got %v", err)
	}
	if deleteCalled {
		t.Fatalf("expected DeleteObjectStorageBucket not to be called when bucket name is empty")
	}
}

func TestDeleteBucket_HappyPath(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{Label: "demo-app-key", ID: 42}}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			return nil
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			return nil
		},
	})

	if err := m.DeleteBucket(context.Background()); err != nil {
		t.Fatalf("DeleteBucket returned error: %v", err)
	}
}

func TestDeleteBucket_StillDeletesBucketWhenAccessKeyCleanupFails(t *testing.T) {
	bucketDeleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return nil, errors.New("list failed")
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			bucketDeleteCalled = true
			return nil
		},
	})

	if err := m.DeleteBucket(context.Background()); err != nil {
		t.Fatalf("expected DeleteBucket to succeed despite access key cleanup failure, got %v", err)
	}
	if !bucketDeleteCalled {
		t.Fatalf("expected bucket deletion to proceed even when access key cleanup fails")
	}
}

func TestDeleteBucket_PropagatesBucketDeletionError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		deleteObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) error {
			return errors.New("delete bucket failed")
		},
	})

	if err := m.DeleteBucket(context.Background()); err == nil {
		t.Fatalf("expected error from DeleteBucket, got nil")
	}
}
