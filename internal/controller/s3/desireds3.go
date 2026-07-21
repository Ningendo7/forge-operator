package s3storage

import (
	"context"
	"fmt"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
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

	if err := m.ReconcileAppIRSA(ctx); err != nil {
		return fmt.Errorf("failed to reconcile app IRSA: %w", err)
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
					ID:     aws.String("StandardLifecycleRule"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilterMemberPrefix{
						Value: "",
					},
					AbortIncompleteMultipartUpload: &s3types.AbortIncompleteMultipartUpload{
						DaysAfterInitiation: aws.Int32(7),
					},
					NoncurrentVersionExpiration: &s3types.NoncurrentVersionExpiration{
						Days: aws.Int32(30),
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
) error {

	roleName := fmt.Sprintf("app-irsa-%s", m.app.Name)

	// Clean up oidcUrl so it works safely in IAM Condition keys
    	oidcHost := strings.TrimPrefix(m.oidcUrl, "https://")

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
	}`, m.oidcArn, oidcHost, m.app.Namespace, m.app.Spec.ServiceAccountName, oidcHost)

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
				return fmt.Errorf("failed to create app IRSA role %s: %w", roleName, err)
			}
			roleArn = aws.ToString(createRoleOut.Role.Arn)
		} else {
			return fmt.Errorf("failed to get app IRSA role %s: %w", roleName, err)
		}
	} else {
		roleArn = aws.ToString(getRoleOut.Role.Arn)
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
		return fmt.Errorf("failed to attach S3 bucket access policy to role %s: %w", roleName, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("App IRSA role %s reconciled successfully with S3 bucket access", roleName))
	

	// Annotate the App/s ServiceAccount
	sa := &corev1.ServiceAccount{}
	err = m.k8sClient.Get(ctx, types.NamespacedName{
		Name:      m.app.Spec.ServiceAccountName,
		Namespace: m.app.Namespace,
	}, sa)
	if err != nil {
		return fmt.Errorf("failed to get ServiceAccount %s/%s: %w", m.app.Namespace, m.app.Spec.ServiceAccountName, err)
	}

	if sa.Annotations == nil {
		sa.Annotations = make(map[string]string)
	}

	// Check if the annotation already exists to prevent unnecessary writes
	if sa.Annotations["eks.amazonaws.com/role-arn"] != roleArn {
		sa.Annotations["eks.amazonaws.com/role-arn"] = roleArn
		if err := m.k8sClient.Update(ctx, sa); err != nil {
			return fmt.Errorf("failed to annotate ServiceAccount %s/%s with role ARN: %w", m.app.Namespace, m.app.Spec.ServiceAccountName, err)
		}

		log.FromContext(ctx).Info(fmt.Sprintf("Annotated ServiceAccount %s/%s with role ARN %s", m.app.Namespace, m.app.Spec.ServiceAccountName, roleArn))
	} else {
		log.FromContext(ctx).Info(fmt.Sprintf("ServiceAccount %s/%s already annotated with role ARN %s", m.app.Namespace, m.app.Spec.ServiceAccountName, roleArn))
	}

	return nil
}