package akamaiobjstr

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3sdktypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
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
//
// verifyOwnershipAndEmptyBucket runs first and gates everything else: if it
// can't confirm this Application still genuinely owns the bucket, nothing
// gets touched at all -- not the bucket, not the access key -- rather than
// partially cleaning up around an uncertain resource. See its own doc
// comment for why.

func (m *Manager) DeleteBucket(
	ctx context.Context,
) (accessKeyErr error, err error) {

	if m.bucket == "" {
		return nil, nil
	}

	logger := logf.FromContext(ctx)

	if err := m.verifyOwnershipAndEmptyBucket(ctx); err != nil {
		return nil, fmt.Errorf("refusing to delete bucket %s: %w", m.bucket, err)
	}

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

// verifyOwnershipAndEmptyBucket confirms this Application still genuinely
// owns the bucket -- via the same ownership marker claimOrVerifyOwnership
// checks on the create path -- before deleting anything in it, then empties
// it (Linode's own DeleteObjectStorageBucket call fails outright if the
// bucket isn't already empty, and this operator's own marker object means
// no bucket it ever claims is ever naturally empty on its own).
//
// Deliberately never claims/writes here, unlike claimOrVerifyOwnership: a
// missing or mismatched marker during cleanup means ownership can't be
// confirmed, so this refuses to proceed rather than guess -- deleting a
// cloud resource on uncertain ownership is worse than getting stuck and
// requiring a human to sort it out (surfaced to them via the Degraded
// condition/BucketCleanupFailed status this error propagates into).
func (m *Manager) verifyOwnershipAndEmptyBucket(ctx context.Context) (err error) {
	bucket, err := m.akamaiClient.GetObjectStorageBucket(ctx, m.region, m.bucket)
	if err != nil {
		if linodego.IsNotFound(err) {
			return nil // Already gone -- nothing to verify or empty.
		}
		return fmt.Errorf("failed to look up bucket %s: %w", m.bucket, err)
	}

	keyResult, err := m.ensureAccessKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to obtain access key to verify/empty bucket %s: %w", m.bucket, err)
	}

	// If ownership ultimately can't be confirmed below, the access key just
	// ensured above is useless -- this Application will never be permitted
	// to touch this bucket -- so clean it up here rather than leaking it
	// indefinitely (nothing else ever will: the finalizer never gets this
	// far again once it knows the bucket isn't its own). Scoped to
	// ErrBucketNotOwned specifically: any other error here (a transient
	// lookup/verify failure) is worth retrying with this same key, not one
	// worth discarding.
	defer func() {
		if errors.Is(err, ErrBucketNotOwned) {
			if cleanupErr := m.deleteApplicationAccessKey(ctx); cleanupErr != nil {
				logf.FromContext(ctx).Error(cleanupErr, "Failed to clean up access key after ownership verification failed", "bucket", m.bucket)
			}
		}
	}()

	s3Client := m.s3ClientFor(m.resolveEndpoint(bucket), keyResult.AccessKey, keyResult.SecretKey)

	out, err := s3Client.GetObject(ctx, &s3sdk.GetObjectInput{
		Bucket: aws.String(m.bucket),
		Key:    aws.String(ownerMarkerKey),
	})
	if err != nil {
		var noSuchKey *s3sdktypes.NoSuchKey
		if errors.As(err, &noSuchKey) {
			if !m.previouslyCreatedByUs() && !m.adoptBucketRequested() {
				return fmt.Errorf("%w: marker missing and this Application has no durable record of creating or adopting it", ErrBucketNotOwned)
			}
		} else {
			return fmt.Errorf("%w: could not verify ownership marker before deletion: %v", ErrBucketNotOwned, err)
		}
	} else {
		body, readErr := io.ReadAll(out.Body)
		_ = out.Body.Close()
		if readErr != nil {
			return fmt.Errorf("failed to read ownership marker: %w", readErr)
		}
		if string(body) != string(m.app.UID) {
			return fmt.Errorf("%w: marker names a different Application", ErrBucketNotOwned)
		}
	}

	return m.emptyBucket(ctx, s3Client)
}

// emptyBucket deletes every object in the bucket. Ownership has already
// been confirmed by the caller -- this never re-checks it.
func (m *Manager) emptyBucket(ctx context.Context, s3Client s3ObjectAPI) error {
	paginator := s3sdk.NewListObjectsV2Paginator(s3Client, &s3sdk.ListObjectsV2Input{
		Bucket: aws.String(m.bucket),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects in bucket '%s': %w", m.bucket, err)
		}

		if len(page.Contents) == 0 {
			continue
		}

		objects := make([]s3sdktypes.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objects = append(objects, s3sdktypes.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		out, err := s3Client.DeleteObjects(ctx, &s3sdk.DeleteObjectsInput{
			Bucket: aws.String(m.bucket),
			Delete: &s3sdktypes.Delete{
				Objects: objects,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("failed batch delete call in bucket %s: %w", m.bucket, err)
		}
		if len(out.Errors) > 0 {
			return fmt.Errorf("failed to delete %d objects in bucket %s (first error: %v)", len(out.Errors), m.bucket, aws.ToString(out.Errors[0].Message))
		}
	}

	return nil
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
