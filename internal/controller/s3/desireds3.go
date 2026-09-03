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

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

const ownerTagKey = "forge-operator.ningendo7.github.io/owner-uid"

// noSuchTagSetErrorCode is the S3 API error code for a bucket with no tags
// at all -- the one GetBucketTagging failure claimOrVerifyOwnership treats
// as safe to claim rather than a hard failure.
const noSuchTagSetErrorCode = "NoSuchTagSet"

// awsIAMRoleNameMaxLen is AWS's own hard limit on IAM role name length.
const awsIAMRoleNameMaxLen = 64

// s3BucketAccessPolicyName is the inline policy name used both when
// attaching this policy (here) and when detaching it during cleanup
// (cleanup.go) -- these must stay identical, or cleanup's DeleteRolePolicy
// targets a policy that was never created, and the still-attached real
// policy then makes DeleteRole fail permanently (roles can't be deleted
// with inline policies still attached), stranding the finalizer forever.
const s3BucketAccessPolicyName = "S3BucketAccessPolicy"

// irsaRoleName builds this Application's per-app IRSA role name. Must be
// used identically in both desireds3.go and cleanup.go: the Application's
// namespace has to be folded in because IAM role names are unique per AWS
// account, not per Kubernetes namespace, so two same-named Applications in
// different namespaces would otherwise collide on one shared role and
// repeatedly overwrite each other's trust policy and bucket-access policy.
func (m *Manager) irsaRoleName() string {
	return naming.CloudResourceName([]string{"app-irsa", m.app.Namespace, m.app.Name}, awsIAMRoleNameMaxLen)
}

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

// recordBucketCreated durably records, in Application.Status, that this
// operator itself just created this bucket -- written immediately after
// CreateBucket succeeds, before ownership tagging is even attempted, so a
// transient failure in that later step doesn't erase the record.
func (m *Manager) recordBucketCreated(ctx context.Context) error {
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   m.bucket,
		Created:  true,
	}
	if err := m.k8sClient.Status().Update(ctx, m.app); err != nil {
		return fmt.Errorf("failed to record bucket creation for %s: %w", m.bucket, err)
	}
	return nil
}

// previouslyCreatedByUs reports whether Application.Status durably records
// this operator having created this exact bucket in an earlier reconcile.
func (m *Manager) previouslyCreatedByUs() bool {
	return m.app.Status.Storage != nil &&
		m.app.Status.Storage.Bucket == m.bucket &&
		m.app.Status.Storage.Created
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
		if err := m.recordBucketCreated(ctx); err != nil {
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
			if err := m.recordBucketCreated(ctx); err != nil {
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
// same call): no ownership tag at all -> claim it only if we durably
// recorded creating this bucket ourselves, or the adopt annotation is set;
// a tag present but naming a different Application -> claim only if the
// adopt annotation is set, otherwise ErrBucketNotOwned; a tag matching this
// Application -> already ours, proceed.
func (m *Manager) claimOrVerifyOwnership(ctx context.Context) error {
	out, err := m.s3client.GetBucketTagging(ctx, &s3sdk.GetBucketTaggingInput{
		Bucket: aws.String(m.bucket),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == noSuchTagSetErrorCode {
			if m.previouslyCreatedByUs() || m.adoptBucketRequested() {
				return m.tagAsOwned(ctx)
			}
			return ErrBucketNotOwned
		}
		return fmt.Errorf("%w: could not verify ownership tag: %v", ErrBucketNotOwned, err)
	}

	for _, tag := range out.TagSet {
		if aws.ToString(tag.Key) == ownerTagKey {
			if aws.ToString(tag.Value) == string(m.app.UID) {
				log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s already exists and is owned by this Application", m.bucket))
				return nil
			}
			if m.adoptBucketRequested() {
				log.FromContext(ctx).Info(fmt.Sprintf("Bucket %s owned by a different Application, adopting per %s annotation", m.bucket, naming.AdoptBucketAnnotation))
				return m.tagAsOwned(ctx)
			}
			return ErrBucketNotOwned
		}
	}

	// Tag set exists (so no NoSuchTagSet error) but carries no ownership tag.
	if m.previouslyCreatedByUs() || m.adoptBucketRequested() {
		return m.tagAsOwned(ctx)
	}
	return ErrBucketNotOwned
}

// adoptBucketRequested reports whether the Application has explicitly opted
// in to taking over a bucket owned by a different Application, via
// naming.AdoptBucketAnnotation.
func (m *Manager) adoptBucketRequested() bool {
	return m.app.Annotations[naming.AdoptBucketAnnotation] == "true"
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

// defaultLifecycleRuleID is the ID stamped on the single implicit rule
// applied when spec.storage.aws.lifecycleRules is left entirely unset.
const defaultLifecycleRuleID = "StandardLifecycleRule"

func (m *Manager) versioningEnabled() bool {
	if m.storage.AWS != nil && m.storage.AWS.VersioningEnabled != nil {
		return *m.storage.AWS.VersioningEnabled
	}
	return true // default to enabled if not specified
}

func (m *Manager) ensureVersioning(
	ctx context.Context,
) error {

	status := s3types.BucketVersioningStatusSuspended
	if m.versioningEnabled() {
		status = s3types.BucketVersioningStatusEnabled
	}

	_, err := m.s3client.PutBucketVersioning(ctx, &s3sdk.PutBucketVersioningInput{
		Bucket: aws.String(m.bucket),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: status,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to set versioning (%s) for bucket %s: %w", status, m.bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Versioning set to %s for bucket %s", status, m.bucket))
	return nil
}

// defaultLifecycleRules - lifecycle policy: abort incomplete multipart uploads at 7d, expire old
// versions at 30d, transition to Standard-IA at 30d. Applied only when
// spec.storage.aws.lifecycleRules is left entirely unset (nil) -- an
// explicitly empty list opts out of this default rather than falling back
// to it.
func (m *Manager) defaultLifecycleRules() []forgev1alpha1.LifecycleRule {
	abortDays := int32(7)
	noncurrentExpDays := int32(30)
	transitionDays := int32(30)

	return []forgev1alpha1.LifecycleRule{
		{
			ID:                                 defaultLifecycleRuleID,
			AbortIncompleteMultipartUploadDays: &abortDays,
			NoncurrentVersionExpirationDays:    &noncurrentExpDays,
			Transitions: []forgev1alpha1.LifecycleTransition{
				{
					Days:         transitionDays,
					StorageClass: string(s3types.TransitionStorageClassStandardIa),
				},
			},
		},
	}
}

// desiredLifecycleRules resolves the rules to apply this reconcile: the
// user's own spec.storage.aws.lifecycleRules if the field was set at all
// (even to an empty list, meaning "no lifecycle policy"), otherwise the
// operator's built-in default.
func (m *Manager) desiredLifecycleRules() []forgev1alpha1.LifecycleRule {
	if m.storage.AWS != nil && m.storage.AWS.LifecycleRules != nil {
		return m.storage.AWS.LifecycleRules
	}
	return m.defaultLifecycleRules()
}

func (m *Manager) ensureLifecyclePolicy(
	ctx context.Context,
) error {

	rules := m.desiredLifecycleRules()

	if len(rules) == 0 {
		// Explicitly opted out: remove any lifecycle policy this operator
		// may have set previously, rather than leaving a stale one active.
		_, err := m.s3client.DeleteBucketLifecycle(ctx, &s3sdk.DeleteBucketLifecycleInput{
			Bucket: aws.String(m.bucket),
		})
		if err != nil && !isNotFoundError(err) {
			return fmt.Errorf("failed to remove lifecycle policy for bucket %s: %w", m.bucket, err)
		}
		log.FromContext(ctx).Info(fmt.Sprintf("No lifecycle rules configured for bucket %s, policy removed", m.bucket))
		return nil
	}

	s3Rules := make([]s3types.LifecycleRule, 0, len(rules))
	for i, rule := range rules {
		id := rule.ID
		if id == "" {
			id = fmt.Sprintf("rule-%d", i)
		}

		status := s3types.ExpirationStatusEnabled
		if rule.Enabled != nil && !*rule.Enabled {
			status = s3types.ExpirationStatusDisabled
		}

		s3Rule := s3types.LifecycleRule{
			ID:     aws.String(id),
			Status: status,
			Filter: &s3types.LifecycleRuleFilter{
				Prefix: aws.String(rule.Prefix),
			},
		}

		if rule.ExpirationDays != nil {
			s3Rule.Expiration = &s3types.LifecycleExpiration{
				Days: aws.Int32(*rule.ExpirationDays),
			}
		}
		if rule.NoncurrentVersionExpirationDays != nil {
			s3Rule.NoncurrentVersionExpiration = &s3types.NoncurrentVersionExpiration{
				NoncurrentDays: aws.Int32(*rule.NoncurrentVersionExpirationDays),
			}
		}
		if rule.AbortIncompleteMultipartUploadDays != nil {
			s3Rule.AbortIncompleteMultipartUpload = &s3types.AbortIncompleteMultipartUpload{
				DaysAfterInitiation: aws.Int32(*rule.AbortIncompleteMultipartUploadDays),
			}
		}

		for _, t := range rule.Transitions {
			s3Rule.Transitions = append(s3Rule.Transitions, s3types.Transition{
				Days:         aws.Int32(t.Days),
				StorageClass: s3types.TransitionStorageClass(t.StorageClass),
			})
		}

		s3Rules = append(s3Rules, s3Rule)
	}

	_, err := m.s3client.PutBucketLifecycleConfiguration(ctx, &s3sdk.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(m.bucket),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: s3Rules,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to set lifecycle policy for bucket %s: %w", m.bucket, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("Lifecycle policy set for bucket %s (%d rule(s))", m.bucket, len(s3Rules)))
	return nil
}

func (m *Manager) ReconcileAppIRSA(
	ctx context.Context,
) (string, error) {

	roleName := m.irsaRoleName()

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
		PolicyName:     aws.String(s3BucketAccessPolicyName),
		PolicyDocument: aws.String(s3Policy),
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach S3 bucket access policy to role %s: %w", roleName, err)
	}

	log.FromContext(ctx).Info(fmt.Sprintf("App IRSA role %s reconciled successfully with S3 bucket access", roleName))

	return roleArn, nil
}
