package akamaiobjstr

import (
	"context"
	"fmt"

	"github.com/linode/linodego"
)

// ReconcileBucket orchestrates bucket + key setup.
func (m *Manager) ReconcileBucket(
	ctx context.Context,
) (*StorageResult, error) {

	if err := m.validateStorageSpec(); err != nil {
		return nil, fmt.Errorf("invalid storage spec: %w", err)
	}

	_, err := m.ensureBucketExists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	keyResult, err := m.ensureAccessKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure access key: %w", err)
	}

	return &StorageResult{
		AccessKey: keyResult.AccessKey,
		SecretKey: keyResult.SecretKey,
		Endpoint:  m.resolveEndpoint(),
	}, nil
}

func (m *Manager) validateStorageSpec() error {
	if m.storage == nil {
		return fmt.Errorf("storage spec is nil")
	}
	if m.bucket == "" {
		return fmt.Errorf("bucket name is empty")
	}
	if m.region == "" {
		return fmt.Errorf("region/cluster is empty")
	}
	return nil
}

func (m *Manager) ensureBucketExists(
	ctx context.Context,
) (*linodego.ObjectStorageBucket, error) {

	bucket, err := m.akamaiClient.GetObjectStorageBucket(ctx, m.region, m.bucket)
	if err == nil {
		return bucket, nil
	}

	if !linodego.IsNotFound(err) {
		return nil, fmt.Errorf("failed to query bucket: %w", err)
	}

	// Bucket does not exist, create it
	createOpts := linodego.ObjectStorageBucketCreateOptions{
		Label:   m.bucket,
		Cluster: m.region,
	}

	newBucket, err := m.akamaiClient.CreateObjectStorageBucket(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	return newBucket, nil
}

func (m *Manager) ensureAccessKey(
	ctx context.Context,
) (*AccessKeyResult, error) {

	keyLabel := fmt.Sprintf("%s-key", m.app.Name)

	keys, err := m.akamaiClient.ListObjectStorageKeys(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list storage keys: %w", err)
	}

	// Reuse existing key if it exists
	for _, key := range keys {
		if key.Label == keyLabel {
			return &AccessKeyResult{
				AccessKey: key.AccessKey,
				SecretKey: "",
			}, nil
		}
	}

	// Create a new scoped access key. BucketName is what actually confines
	// this key to the Application's own bucket rather than every bucket in
	// the account/region — it's a required field on Linode's side (no
	// omitempty on the wire type), so leaving it unset previously meant this
	// wasn't achieving the least-privilege scoping the BucketAccess field
	// exists for.
	perm := linodego.ObjectStorageKeyBucketAccess{
		Cluster:     m.region,
		BucketName:  m.bucket,
		Permissions: "read_write",
	}

	createOpts := linodego.ObjectStorageKeyCreateOptions{
		Label:        keyLabel,
		BucketAccess: &[]linodego.ObjectStorageKeyBucketAccess{perm},
	}

	key, err := m.akamaiClient.CreateObjectStorageKey(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage key: %w", err)
	}

	return &AccessKeyResult{
		AccessKey: key.AccessKey,
		SecretKey: key.SecretKey,
	}, nil
}

func (m *Manager) resolveEndpoint() string {
	if m.storage.Endpoint != "" {
		return m.storage.Endpoint
	}
	return fmt.Sprintf("%s.linodeobjects.com", m.region)
}
