package s3storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"

	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// isNotFoundError checks for a missing-bucket error across its several AWS SDK forms.
func isNotFoundError(err error) bool {
	var noSuchBucketErr *s3types.NoSuchBucket
	if errors.As(err, &noSuchBucketErr) {
		return true
	}

	var responseErr *awshttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == 404 {
		return true
	}

	return false
}

// CleanupBucket deletes the bucket and this Application's own IRSA role.
// irsaErr surfaces a role-cleanup failure to the caller (for an Event,
// status, or similar) without making it fatal to the rest of cleanup --
// it's returned separately from err, which is only ever the bucket
// deletion's own error. Mirrors the identical accessKeyErr/err split on
// the Akamai package's DeleteBucket, for the same reason.
//
// IRSA cleanup deliberately runs before, and independently of, the bucket
// ownership check below: the role's identity is entirely deterministic
// (irsaRoleName derives it from this Application's own namespace/name),
// never ambiguous the way a bucket's ownership can be, so there's nothing
// to verify before removing it. Confirmed live as a real gap: once a
// bucket is adopted away from this Application via the adopt-bucket
// annotation, verifyOwnership will correctly and permanently refuse to
// touch that bucket for this Application from then on -- which, when IRSA
// cleanup was gated behind that same check, meant this Application could
// never clean up its own IAM role again either, leaking it indefinitely
// even though the role itself was never in dispute.
func (m *Manager) CleanupBucket(
	ctx context.Context,
) (irsaErr error, err error) {

	if cleanupErr := m.cleanupAppIRSA(ctx); cleanupErr != nil {
		irsaErr = fmt.Errorf("failed to cleanup IRSA: %w", cleanupErr)
	}

	// Never delete the bucket on uncertain ownership: confirmed via the
	// same tag check claimOrVerifyOwnership uses on the create path, but
	// never claims/writes here. A missing or mismatched tag means this
	// refuses to touch the bucket at all, surfaced as a failed cleanup for
	// a human to resolve rather than risk deleting something this
	// Application doesn't actually own.
	if err := m.verifyOwnership(ctx); err != nil {
		return irsaErr, fmt.Errorf("refusing to delete bucket %s: %w", m.bucket, err)
	}

	// Delete objects in the bucket before deleting the bucket itself
	if err := m.deleteAllObjectVersions(ctx); err != nil {
		return irsaErr, fmt.Errorf("failed to delete objects in bucket %s: %w", m.bucket, err)
	}

	// Abort any in-progress multipart uploads so they don't linger after the bucket is gone
	if err := m.abortMultipartUploads(ctx); err != nil {
		return irsaErr, fmt.Errorf("failed to abort multipart uploads in bucket %s: %w", m.bucket, err)
	}

	// Now delete the bucket
	if err := m.deleteBucket(ctx); err != nil {
		return irsaErr, fmt.Errorf("failed to delete bucket %s: %w", m.bucket, err)
	}

	return irsaErr, nil
}

// verifyOwnership re-checks the bucket's ownership tag, reusing exactly
// noSuchTagSetErrorCode/ownerTagKey/previouslyCreatedByUs/
// adoptBucketRequested from desireds3.go (same package). A bucket that's
// already gone entirely (noSuchBucketErrorCode, distinct from
// noSuchTagSetErrorCode's "exists but untagged") is treated as a
// successful no-op here -- confirmed live as a real gap otherwise: without
// this, retrying cleanup on a bucket a previous attempt had already
// successfully deleted wrongly reported ErrBucketNotOwned for a bucket
// that was, in fact, correctly cleaned up already.
func (m *Manager) verifyOwnership(ctx context.Context) error {
	out, err := m.s3client.GetBucketTagging(ctx, &s3sdk.GetBucketTaggingInput{
		Bucket: aws.String(m.bucket),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == noSuchBucketErrorCode {
			// Already gone -- nothing to verify or clean up. Confirmed
			// live, via this package's own integration test: without this
			// check, retrying cleanup on a bucket a previous attempt had
			// already successfully deleted (e.g. after a transient error
			// removing the finalizer itself, or a controller restart
			// mid-flight) wrongly reported ErrBucketNotOwned for a bucket
			// that was in fact correctly cleaned up already, permanently
			// stuck-failing an Application's deletion for no real reason.
			return nil
		}
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == noSuchTagSetErrorCode {
			if m.previouslyCreatedByUs() || m.adoptBucketRequested() {
				return nil
			}
			return fmt.Errorf("%w: no ownership tag and no durable record of creating or adopting it", ErrBucketNotOwned)
		}
		return fmt.Errorf("%w: could not verify ownership tag before deletion: %v", ErrBucketNotOwned, err)
	}

	for _, tag := range out.TagSet {
		if aws.ToString(tag.Key) == ownerTagKey {
			if aws.ToString(tag.Value) == string(m.app.UID) {
				return nil
			}
			return fmt.Errorf("%w: ownership tag names a different Application", ErrBucketNotOwned)
		}
	}

	if m.previouslyCreatedByUs() || m.adoptBucketRequested() {
		return nil
	}
	return fmt.Errorf("%w: no ownership tag and no durable record of creating or adopting it", ErrBucketNotOwned)
}

func (m *Manager) deleteAllObjectVersions(
	ctx context.Context,
) error {

	paginator := s3sdk.NewListObjectVersionsPaginator(
		m.s3client,
		&s3sdk.ListObjectVersionsInput{
			Bucket: aws.String(m.bucket),
		},
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {

			// Handle case where the bucket is not found or has been deleted
			var noSuchBucketErr *s3types.NoSuchBucket
			if errors.As(err, &noSuchBucketErr) {
				return nil // Bucket does not exist, nothing to delete
			}
			return fmt.Errorf("failed to list object versions in bucket %s: %w", m.bucket, err)
		}

		var objectsToDelete []s3types.ObjectIdentifier
		for _, version := range page.Versions {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key:       version.Key,
				VersionId: version.VersionId,
			})
		}

		for _, marker := range page.DeleteMarkers {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key:       marker.Key,
				VersionId: marker.VersionId,
			})
		}

		if len(objectsToDelete) == 0 {
			continue
		}

		// Chunk the batch deletion into batches of 1000 objects to avoid exceeding AWS limits
		batchSize := 1000
		for i := 0; i < len(objectsToDelete); i += batchSize {
			end := min(i+batchSize, len(objectsToDelete))

			out, err := m.s3client.DeleteObjects(ctx, &s3sdk.DeleteObjectsInput{
				Bucket: aws.String(m.bucket),
				Delete: &s3types.Delete{
					Objects: objectsToDelete[i:end],
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				return fmt.Errorf("failed batch delete call in bucket %s: %w", m.bucket, err)
			}

			// Catch per-object errors in the response:
			if len(out.Errors) > 0 {
				return fmt.Errorf("failed to delete %d objects in bucket %s (first error: %v)", len(out.Errors), m.bucket, aws.ToString(out.Errors[0].Message))
			}
		}
	}

	return nil
}

func (m *Manager) abortMultipartUploads(
	ctx context.Context,
) error {

	uploads, err := m.s3client.ListMultipartUploads(ctx, &s3sdk.ListMultipartUploadsInput{
		Bucket: aws.String(m.bucket),
	})
	if err != nil {
		if isNotFoundError(err) {
			return nil // Bucket does not exist, nothing to abort
		}
		return fmt.Errorf("failed to list multipart uploads in bucket %s: %w", m.bucket, err)
	}

	for _, upload := range uploads.Uploads {
		_, _ = m.s3client.AbortMultipartUpload(ctx, &s3sdk.AbortMultipartUploadInput{
			Bucket:   aws.String(m.bucket),
			Key:      upload.Key,
			UploadId: upload.UploadId,
		})
	}
	return nil
}

func (m *Manager) deleteBucket(
	ctx context.Context,
) error {

	_, err := m.s3client.DeleteBucket(ctx, &s3sdk.DeleteBucketInput{
		Bucket: aws.String(m.bucket),
	})
	if err != nil {
		if isNotFoundError(err) {
			return nil // Bucket does not exist, nothing to delete
		}
		return fmt.Errorf("failed to delete bucket %s: %w", m.bucket, err)
	}

	return nil
}

func (m *Manager) cleanupAppIRSA(
	ctx context.Context,
) error {

	roleName := m.irsaRoleName()

	// Delete the inline policy attached to the role. Must be the exact same
	// name used to attach it in desireds3.go's ReconcileAppIRSA -- see the
	// comment on s3BucketAccessPolicyName for what goes wrong if these ever
	// drift apart again.
	_, err := m.iamclient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(s3BucketAccessPolicyName),
	})
	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if !errors.As(err, &noSuchEntity) {
			return fmt.Errorf("failed to delete inline policy for role %s: %w", roleName, err)
		}
	}

	// Delete the role itself
	_, err = m.iamclient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})

	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if errors.As(err, &noSuchEntity) {
			return nil // Role not found, consider it deleted
		}
		return fmt.Errorf("failed to delete IAM role %s: %w", roleName, err)
	}

	return nil
}
