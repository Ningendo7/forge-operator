package s3storage

import (
	"context"
	"fmt"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// 
func (m *Manager) CleanupBucket(
	ctx context.Context,
) error {

	// Hanle IRSA cleanup first
	if err := m.cleanupAppIRSA(ctx); err != nil {
		return fmt.Errorf("failed to cleanup IRSA: %w", err)
	}

	// Delete objects in the bucket before deleting the bucket itself
	if err := m.deleteAllObjectVersions(ctx); err != nil {
		return fmt.Errorf("failed to delete objects in bucket %s: %w", m.bucket, err)
	}

	// Now delete the bucket
	if err := m.deleteBucket(ctx); err != nil {
		return fmt.Errorf("failed to delete bucket %s: %w", m.bucket, err)
	}

	return nil
}

func (m *Manager) deleteAllObjectVersions(
	ctx context.Context,
) error {

	paginator := s3sdk.NewListObjectsVersionsPaginator(
		m.s3client, 
		&s3sdk.ListObjectsVersionsInput{
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
			end := i + batchSize
			if end > len(objectsToDelete) {
				end = len(objectsToDelete)
			}

			_, err := m.s3client.DeleteObjects(ctx, &s3sdk.DeleteObjectsInput{
				Bucket: aws.String(m.bucket),
				Delete: &s3types.Delete{
					Objects: objectsToDelete[i:end],
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				return fmt.Errorf("failed to delete objects version in bucket %s: %w", m.bucket, err)
			}
		}
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
		var noSuchBucketErr *s3types.NoSuchBucket
		if errors.As(err, &noSuchBucketErr) {
			return nil // Bucket does not exist, nothing to delete
		}
		return fmt.Errorf("failed to delete bucket %s: %w", m.bucket, err)
	}

	return nil
}

func (m *Manager) cleanupAppIRSA(
	ctx context.Context,
) error {

	roleName := fmt.Sprintf("app-irsa-%s", m.app.Name)

	// Delete the inline policy attached to the role
	_, err = m.iamclient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(fmt.Sprintf("app-irsa-policy-%s", m.app.Name)),
	})
	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if !errors.As(err, &noSuchEntity) {
			return fmt.Errorf("failed to delete inline policy for role %s: %w", roleName, err)
		}
	}

	// Delete the role itself
	_, err := m.iamclient.DeleteRole(ctx, &iam.DeleteRoleInput{
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
