package akamaiobjstr

import (
	"context"
	"fmt"

	"github.com/linode/linodego"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (m *Manager) DeleteBucket(
	ctx context.Context,
) error {

	if m.bucket == "" {
		return nil
	}

	logger := logf.FromContext(ctx)

	if err := m.deleteApplicationAccessKey(ctx); err != nil {
		logger.Error(err, "Failed to clean up access key during finalization", "bucket", m.bucket)
	}

	if err := m.deleteStorageBucket(ctx); err != nil {
		return err
	}

	logger.Info("Successfully finalized Akamai Object Storage resources", "bucket", m.bucket)
	return nil
}

func (m *Manager) deleteApplicationAccessKey(
	ctx context.Context,
) error {

	keyLabel := fmt.Sprintf("%s-key", m.app.Name)

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
