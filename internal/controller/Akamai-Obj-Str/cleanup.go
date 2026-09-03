package akamaiobjstr

import (
	"context"
	"fmt"

	"github.com/linode/linodego"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DeleteBucket deletes the bucket and its access key. Bucket deletion is
// deliberately never blocked by an access-key cleanup failure -- the bucket
// is the billed resource, the access key is not, so guaranteeing the costly
// one gets deleted takes priority over a transient failure on the free one.
// accessKeyErr surfaces that failure to the caller (for an Event, status, or
// similar) without making it fatal: it's returned separately from err,
// which is only ever the bucket deletion's own error.
func (m *Manager) DeleteBucket(
	ctx context.Context,
) (accessKeyErr error, err error) {

	if m.bucket == "" {
		return nil, nil
	}

	logger := logf.FromContext(ctx)

	if keyErr := m.deleteApplicationAccessKey(ctx); keyErr != nil {
		logger.Error(keyErr, "Failed to clean up access key during finalization", "bucket", m.bucket)
		accessKeyErr = keyErr
	}

	if err := m.deleteStorageBucket(ctx); err != nil {
		return accessKeyErr, err
	}

	logger.Info("Successfully finalized Akamai Object Storage resources", "bucket", m.bucket)
	return accessKeyErr, nil
}

func (m *Manager) deleteApplicationAccessKey(
	ctx context.Context,
) error {

	keyLabel := m.accessKeyLabel()

	keyID, err := m.findAccessKeyIDByLabel(ctx, keyLabel)
	if err != nil {
		return fmt.Errorf("failed to locate access key '%s': %w", keyLabel, err)
	}

	if keyID == 0 {
		return nil // Key already removed or does not exist
	}

	if err := m.akamaiClient.DeleteObjectStorageKey(ctx, keyID); err != nil {
		return fmt.Errorf("failed to delete access key '%d': %w", keyID, err)
	}

	return nil
}

func (m *Manager) findAccessKeyIDByLabel(
	ctx context.Context,
	label string,
) (int, error) {

	keys, err := m.akamaiClient.ListObjectStorageKeys(ctx, nil)
	if err != nil {
		return 0, err
	}

	for _, k := range keys {
		if k.Label == label {
			return k.ID, nil
		}
	}

	return 0, nil
}

func (m *Manager) deleteStorageBucket(
	ctx context.Context,
) error {

	err := m.akamaiClient.DeleteObjectStorageBucket(ctx, m.region, m.bucket)
	if err != nil && !linodego.IsNotFound(err) {
		return fmt.Errorf("failed to delete bucket '%s': %w", m.bucket, err)
	}

	return nil
}
