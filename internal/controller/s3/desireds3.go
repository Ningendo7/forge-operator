package s3storage

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awstypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type S3Client struct {

	S3API *awss3.Client

}

func (s *S3Client) ensureVersioning(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	bucket := application.Spec.Storage.Bucket

	_, err := s.S3API.PutBucketVersioning(ctx, &awss3.PutBucketVersioningInput{
		Bucket: &bucket,
		VersioningConfiguration: &awstypes.VersioningConfiguration{
			Status: awstypes.BucketVersioningStatusEnabled,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to enable versioning for bucket %s: %w", bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Versioning enabled for bucket %s", bucket))
	return nil

}

func (s *S3Client) ensureLifecyclePolicy(
	ctx context.Context, 
	application *forgev1alpha1.Application,
) error {

	bucket := application.Spec.Storage.Bucket

	_, err := s.S3API.PutBucketLifecycleConfiguration(ctx, &awss3.PutBucketLifecycleConfigurationInput{
		Bucket: &bucket,
		LifecycleConfiguration: &awstypes.BucketLifecycleConfiguration{
			Rules: []awstypes.LifecycleRule{
				{
					ID:     aws.String("ExpireOldVersions"),
					Status: awstypes.ExpirationStatusEnabled,
					NoncurrentVersionExpiration: &awstypes.NoncurrentVersionExpiration{
						Days: 30,
					},
				},
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to set lifecycle policy for bucket %s: %w", bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Lifecycle policy set for bucket %s", bucket))
	return nil
}