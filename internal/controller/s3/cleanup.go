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
		_, err := m.s3client.DeleteObjects(ctx, &s3sdk.DeleteObjectsInput{
			Bucket: aws.String(m.bucket),
			Delete: &s3types.Delete{
				Objects: objectsToDelete,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to delete objects version in bucket %s: %w", m.bucket, err)
		}
	}

	return nil
}

