package s3storage

import (
	"context"
	"fmt"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (m *Manager) ReconcileBucket(
	ctx context.Context,
) error {

	if err := m.ensureBucketExists(ctx); err != nil {
		return fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	if err := m.ensureVersioning(ctx); err != nil {
		return fmt.Errorf("failed to ensure versioning: %w", err)
	}

	if err := m.ensureLifecyclePolicy(ctx); err != nil {
		return fmt.Errorf("failed to ensure lifecycle policy: %w", err)
	}

	return nil

}
func (m *Manager) ensureBucketExists(
	ctx context.Context,
) error {

	_, err := m.s3client.HeadBucket(ctx, &s3sdk.HeadBucketInput{
		Bucket: aws.String(m.bucket),
	})

	if err == nil {
		log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s already exists", m.bucket))
		return nil
	}

	var responseErr *awshttp.ResponseError
	if errors.As(err, &responseErr) {

		switch responseErr.HTTPStatusCode() {
		case 404:
			log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s does not exist, creating...", m.bucket))
			return m.CreateBucket(ctx)
		case 403:
			return fmt.Errorf("access denied to bucket %s: %w", m.bucket, err)
		case 301:
			return fmt.Errorf("bucket %s is in a different region: %w", m.bucket, err)
		default:
			return fmt.Errorf("unexpected error checking bucket %s: %w", m.bucket, err)
		}
	}
	return err

}

func (m *Manager) CreateBucket(
	ctx context.Context,
) error {

	input := &s3sdk.CreateBucketInput{
		Bucket: aws.String(m.bucket),
	}

	if m.region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(m.region),
		}
	}

	_, err := m.s3client.CreateBucket(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create bucket %s: %w", m.bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s created successfully", m.bucket))
	return nil

}


func (m *Manager) ensureVersioning(
	ctx context.Context,
) error {

	_, err := m.s3client.PutBucketVersioning(ctx, &s3sdk.PutBucketVersioningInput{
		Bucket: aws.String(m.bucket),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: s3types.BucketVersioningStatusEnabled,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to enable versioning for bucket %s: %w", m.bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Versioning enabled for bucket %s", m.bucket))
	return nil

}

func (m *Manager) ensureLifecyclePolicy(
	ctx context.Context,
) error {

	// Standard production lifecycle policy:
	// - Abort incomplete multipart uploads after 7 days
	// - Expire noncurrent versions after 30 days
	// Transition current objects to Standard-IA after 30 days
	input := &s3sdk.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(m.bucket),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{
				{
					ID:     aws.String("CleanupIncompleteMultipartUpload"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilterMemberPrefix{
						Value: "",
					},
					AbortIncompleteMultipartUpload: &s3types.AbortIncompleteMultipartUpload{
						DaysAfterInitiation: aws.Int32(7),
					},
				},
				{
					ID:     aws.String("ExpireOldNoncurrentVersions"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilterMemberPrefix{
						Value: "",
					},
					NoncurrentVersionExpiration: &s3types.NoncurrentVersionExpiration{
						Days: aws.Int32(30),
					},
				},
				{
					ID:     aws.String("TransitionToStandardIA"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilterMemberPrefix{
						Value: "",
					},
					Transitions: []s3types.Transition{
						{
							Days:         aws.Int32(30),
							StorageClass: s3types.TransitionStorageClassStandardIa,
						},
					},
				},
			},
		},
	}

	_, err := m.s3client.PutBucketLifecycleConfiguration(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to set lifecycle policy for bucket %s: %w", m.bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Lifecycle policy set for bucket %s", m.bucket))
	return nil

}