package akamaiobjstr

import (
	"context"
	"fmt"

	"github.com/linode/linodego"
)

// ReconcileBucket acts as the top-level orchestrator pipeline.
// It delegates every operation to focused helper functions.
func (m *Manager) ReconcileBucket(
	ctx context.Context,
) (string, string, string, error) {

	if err := m.validateStorageSpec(); err != nil {
		return "", "", "", fmt.Errorf("invalid storage spec: %w", err)
	}

	bucket, err := m.ensureBucketExists(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	if err := m.ensureVersioning(ctx); err != nil {
		return "", "", "", fmt.Errorf("failed to ensure versioning: %w", err)
	}

	accessKey, secretKey, err := m.ensureAccessKey(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to ensure access key: %w", err)
	}

	endpoint := m.resolveEndpoint()

	return accessKey, secretKey, endpoint, nil
}

func (m *Manager) validateStorageSpec() error {
	if m.storage == nil {
		return fmt.Errorf("storage spec is nil")
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
		Label:  m.bucket,
		Region: m.region,
		LifecycleRule: m.buildLifecycleRule(),
	}

	newBucket, err := m.akamaiClient.CreateObjectStorageBucket(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	return newBucket, nil
}

func (m *Manager) ensureVersioning(
	ctx context.Context,
	bucket *linodego.ObjectStorageBucket,
) error {

	if !m.storage.Versioning {
		return nil
	}

	opts := linodego.ObjectStorageBucketUpdateOptions{
		Versioning: linodego.ObjectStorageBucketVersioningEnabled,
	}

	if _, err := m.akamaiClient.UpdateObjectStorageBucket(ctx, m.region, m.bucket, opts); err != nil {
		return fmt.Errorf("failed to update bucket versioning: %w", err)
	}
	return nil
}

func (m *Manager) ensureAccessKey(
	ctx context.Context,
) (string, string, error) {

	keyLabel := fmt.Sprintf("%s-key", m.app.Name)

	keys, err := m.akamaiClient.ListObjectStorageKeys(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to list storage keys: %w", err)
	}

	// Reuse existing key if it exists
	for _, key := range keys {
		if key.Label == keyLabel {
			return key.AccessKey, "", nil

		}
	}
	// Create a new scoped access key
	perm := linodego.ObjectStorageKeyBucketAccess{
		Bucket: m.bucket,
		Region: m.region,
		Permisions: "read_write",
	}

	createOpts := linodego.ObjectStorageKeyCreateOptions{
		Label:   keyLabel,
		BucketAccess: &[]linodego.ObjectStorageKeyBucketAccess{perm},
	}

	key, err := m.akamaiClient.CreateObjectStorageKey(ctx, createOpts)
	if err != nil {
		return "", "", fmt.Errorf("failed to create storage key: %w", err)
	}

	return key.AccessKey, key.SecretKey, nil
}

func (m *Manager) resolveEndpoint() string {
	if m.storage.Endpoint != "" {
		return m.storage.Endpoint
	}

	return fmt.Sprintf("%s.linodeobjects.com", m.region)
}

func (m *Manager) buildLifecycleRule() *linodego.ObjectStorageBucketLifecycleRule {
	if m.storage.Lifecycle == nil {
		return nil
	}

	rule := linodego.ObjectStorageBucketLifecycleRule{
		ID:     fmt.Sprintf("%s-lifecycle-rule", m.app.Name),
		Enabled: true,
		Prefix: m.storage.Lifecycle.Prefix,
	}

	if m.storage.Lifecycle.ExpirationDays > 0 {
		rule.Expiration = &linodego.ObjectStorageBucketLifecycleExpiration{
			Days: int(m.storage.Lifecycle.ExpirationDays),
		}
	}

	return &[]linodego.ObjectStorageBucketLifecycleRule{rule}
