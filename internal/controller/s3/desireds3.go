package s3storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const ownerTagKey = "forge-operator.ningendo7.github.io/owner-uid"

// noSuchTagSetErrorCode is the S3 API error code for a bucket with no tags
// at all -- the one GetBucketTagging failure claimOrVerifyOwnership treats
// as safe to claim rather than a hard failure.
const noSuchTagSetErrorCode = "NoSuchTagSet"

func (m *Manager) ReconcileBucket(
	ctx context.Context,
) (*StorageResult, error) {

	if err := m.ensureBucketExists(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	if err := m.ensureVersioning(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure versioning: %w", err)
	}

	if err := m.ensureLifecyclePolicy(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure lifecycle policy: %w", err)
	}

	roleArn, err := m.ReconcileAppIRSA(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile app IRSA: %w", err)
	}

	return &StorageResult{
		RoleARN: roleArn,
	}, nil

}

// ErrBucketNotOwned means a bucket with the desired name exists but wasn't
// created by this operator for this Application -- surfaced as a Degraded
// condition rather than silently adopted (and later possibly deleted)
var ErrBucketNotOwned = errors.New("bucket already exists and is not owned by forge-operator")

func (m *Manager) ensureBucketExists(
	ctx context.Context,
) error {

	_, err := m.s3client.HeadBucket(ctx, &s3sdk.HeadBucketInput{
		Bucket: aws.String(m.bucket),
	})

	if err == nil {
		return m.claimOrVerifyOwnership(ctx)
	}

	var notFoundErr *s3types.NotFound
	if errors.As(err, &notFoundErr) {
		log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s does not exist, creating...", m.bucket))
		if err := m.CreateBucket(ctx); err != nil {
			return err
		}
		return m.claimOrVerifyOwnership(ctx)
	}

	var responseErr *awshttp.ResponseError
	if errors.As(err, &responseErr) {

		switch responseErr.HTTPStatusCode() {
		case 404:
			log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s does not exist, creating...", m.bucket))
			if err := m.CreateBucket(ctx); err != nil {
				return err
			}
			return m.claimOrVerifyOwnership(ctx)
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

func (m *Manager) tagAsOwned(ctx context.Context) error {
	_, err := m.s3client.PutBucketTagging(ctx, &s3sdk.PutBucketTaggingInput{
		Bucket: aws.String(m.bucket),
		Tagging: &s3types.Tagging{
			TagSet: []s3types.Tag{
				{Key: aws.String(ownerTagKey), Value: aws.String(string(m.app.UID))},
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to tag bucket %s as owned: %w", m.bucket, err)
	}

	return nil
}

// claimOrVerifyOwnership handles a bucket HeadBucket found to already exist
// (whether it was already there, or we just created it a moment ago in this
// same call): no ownership tag at all -> claim it by writing our tag; a tag
// present but naming a different Application -> ErrBucketNotOwned; a tag
// matching this Application -> already ours, proceed.
//
// Deliberately NOT split into a separate "just created, skip the check"
// path: if tagAsOwned failed transiently right after a real CreateBucket in
// an earlier reconcile, the bucket now exists with no tag on it, and only
// this unified path can recover -- a "did I just create it in this exact
// call" flag would permanently read that bucket as foreign forever after a
// single blip. The residual risk this accepts -- something else claiming
// this exact operator-scoped bucket name in the narrow window before we
// tag it -- is far rarer and lower-consequence than that self-lockout.
func (m *Manager) claimOrVerifyOwnership(ctx context.Context) error {
	out, err := m.s3client.GetBucketTagging(ctx, &s3sdk.GetBucketTaggingInput{
		Bucket: aws.String(m.bucket),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == noSuchTagSetErrorCode {
			return m.tagAsOwned(ctx)
		}
		return fmt.Errorf("%w: could not verify ownership tag: %v", ErrBucketNotOwned, err)
	}

	for _, tag := range out.TagSet {
		if aws.ToString(tag.Key) == ownerTagKey {
			if aws.ToString(tag.Value) == string(m.app.UID) {
				log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s already exists and is owned by this Application", m.bucket))
				return nil
			}
			return ErrBucketNotOwned
		}
	}

	// Tag set exists (so no NoSuchTagSet error) but carries no ownership tag
	// -- same "safe to claim" reasoning as the NoSuchTagSet case above.
	return m.tagAsOwned(ctx)
}

func (m *Manager) CreateBucket(
	ctx context.Context,
) error {

	input := &s3sdk.CreateBucketInput{
		Bucket: aws.String(m.bucket),
	}

	if m.region != defaultRegion {
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

	// Abort stale multipart uploads at 7d, expire old versions and transition to Standard-IA at 30d.
	input := &s3sdk.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(m.bucket),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{
				{
					ID:     aws.String("StandardLifecycleRule"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilter{
						Prefix: aws.String(""),
					},
					AbortIncompleteMultipartUpload: &s3types.AbortIncompleteMultipartUpload{
						DaysAfterInitiation: aws.Int32(7),
					},
					NoncurrentVersionExpiration: &s3types.NoncurrentVersionExpiration{
						NoncurrentDays: aws.Int32(30),
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

func (m *Manager) ReconcileAppIRSA(
	ctx context.Context,
) (string, error) {

	roleName := fmt.Sprintf("app-irsa-%s", m.app.Name)

	// Clean up oidcUrl so it works safely in IAM Condition keys
	oidcHost := strings.TrimPrefix(m.OIDCProviderURL, "https://")
	oidcHost = strings.TrimSuffix(oidcHost, "/") // Remove trailing slash if present

	trustPolicy := fmt.Sprintf(`{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Principal": {
				"Federated": "%s"
			},
			"Action": "sts:AssumeRoleWithWebIdentity",
			"Condition": {
				"StringEquals": {
					"%s:sub": "system:serviceaccount:%s:%s",
					"%s:aud": "sts.amazonaws.com"
				}
			}
		}]
	}`, m.OIDCProviderARN, oidcHost, m.app.Namespace, m.serviceAccountName, oidcHost)

	// Ensure the IAM role exists
	var roleArn string
	getRoleOut, err := m.iamclient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		var notFoundErr *iamtypes.NoSuchEntityException
		if errors.As(err, &notFoundErr) {
			// Assume the role does not exist and create it
			createRoleOut, err := m.iamclient.CreateRole(ctx, &iam.CreateRoleInput{
				RoleName:                 aws.String(roleName),
				AssumeRolePolicyDocument: aws.String(trustPolicy),
			})
			if err != nil {
				return "", fmt.Errorf("failed to create app IRSA role %s: %w", roleName, err)
			}
			roleArn = aws.ToString(createRoleOut.Role.Arn)
		} else {
			return "", fmt.Errorf("failed to get app IRSA role %s: %w", roleName, err)
		}
	} else {
		roleArn = aws.ToString(getRoleOut.Role.Arn)
		// Update the existing trust policy if it has changed
		_, err = m.iamclient.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
			RoleName:       aws.String(roleName),
			PolicyDocument: aws.String(trustPolicy),
		})
		if err != nil {
			return "", fmt.Errorf("failed to update trust policy for app IRSA role %s: %w", roleName, err)
		}

	}

	// Attach Bucket Access Policy to the Role
	s3Policy := fmt.Sprintf(`{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Action": [
				"s3:ListBucket",
				"s3:GetObject",
				"s3:PutObject",
				"s3:DeleteObject"
			],
			"Resource": [
				"arn:aws:s3:::%s",
				"arn:aws:s3:::%s/*"
			]
		}]
	}`, m.bucket, m.bucket)

	_, err = m.iamclient.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyName:     aws.String("S3BucketAccessPolicy"),
		PolicyDocument: aws.String(s3Policy),
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach S3 bucket access policy to role %s: %w", roleName, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("App IRSA role %s reconciled successfully with S3 bucket access", roleName))

	return roleArn, nil
}
