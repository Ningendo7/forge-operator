package s3storage

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// --- ensureBucketExists ---

// matchingTagging returns a GetBucketTaggingOutput carrying the ownership
// tag for testAppUID, i.e. what verifyOwnership sees for a bucket this
// operator created for this Application.
func matchingTagging() *s3sdk.GetBucketTaggingOutput {
	return &s3sdk.GetBucketTaggingOutput{
		TagSet: []s3types.Tag{
			{Key: aws.String(ownerTagKey), Value: aws.String(string(testAppUID))},
		},
	}
}

func TestEnsureBucketExists_OwnedBucketFound(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return matchingTagging(), nil
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected CreateBucket not to be called when bucket already exists")
	}
}

func TestEnsureBucketExists_ReturnsNotOwnedWhenFoundBucketTagMismatched(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
				},
			}, nil
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestEnsureBucketExists_ClaimsOwnershipWhenFoundBucketHasEmptyTagSet(t *testing.T) {
	var taggedOwner string
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			for _, tag := range params.Tagging.TagSet {
				if aws.ToString(tag.Key) == ownerTagKey {
					taggedOwner = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	// A bucket that exists but carries no ownership tag at all is claimed,
	// not rejected -- this is what recovers a bucket this operator created
	// in an earlier reconcile whose tag write failed transiently, since
	// that bucket looks identical to a genuinely foreign untagged one.
	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if taggedOwner != string(testAppUID) {
		t.Fatalf("expected bucket to be claimed with owner UID, got %q", taggedOwner)
	}
}

func TestEnsureBucketExists_ClaimsOwnershipOnNoSuchTagSetError(t *testing.T) {
	claimed := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode, Message: "The TagSet does not exist"}
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			claimed = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !claimed {
		t.Fatalf("expected NoSuchTagSet to be treated as claimable, not rejected")
	}
}

func TestEnsureBucketExists_ReturnsNotOwnedWhenGetTaggingErrors(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			// A genuine, non-NoSuchTagSet failure (permission denied, network
			// error, ...) must NOT be treated as claimable -- only the
			// specific "no tags at all" case is safe to claim.
			return nil, errors.New("access denied")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestEnsureBucketExists_CreatesBucketOnTypedNotFound(t *testing.T) {
	createCalled := false
	var taggedBucket, taggedOwner string
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, &s3types.NotFound{}
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode}
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			taggedBucket = aws.ToString(params.Bucket)
			for _, tag := range params.Tagging.TagSet {
				if aws.ToString(tag.Key) == ownerTagKey {
					taggedOwner = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateBucket to be called when bucket is not found")
	}
	if taggedBucket != testBucket || taggedOwner != string(testAppUID) {
		t.Fatalf("expected newly created bucket to be tagged with owner UID, got bucket=%q owner=%q", taggedBucket, taggedOwner)
	}
}

func TestEnsureBucketExists_CreatesBucketOn404ResponseError(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(404, "not found")
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode}
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateBucket to be called on 404 response error")
	}
}

func TestTagAsOwned_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			return nil, errors.New("tagging denied")
		},
	}, nil)

	if err := m.tagAsOwned(context.Background()); err == nil {
		t.Fatalf("expected error from tagAsOwned, got nil")
	}
}

func TestEnsureBucketExists_ReturnsErrorOn403(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(403, "access denied")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error on 403 response, got nil")
	}
}

func TestEnsureBucketExists_ReturnsErrorOn301(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(301, "wrong region")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error on 301 response, got nil")
	}
}

func TestEnsureBucketExists_ReturnsErrorOnUnexpectedStatus(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(500, "internal error")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error on unexpected status, got nil")
	}
}

// --- CreateBucket ---

func TestCreateBucket_OmitsLocationConstraintForUSEast1(t *testing.T) {
	m := newTestManager(&mockS3Client{
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			if params.CreateBucketConfiguration != nil {
				t.Errorf("expected no CreateBucketConfiguration for us-east-1, got %#v", params.CreateBucketConfiguration)
			}
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)
	m.region = "us-east-1"

	if err := m.CreateBucket(context.Background()); err != nil {
		t.Fatalf("CreateBucket returned error: %v", err)
	}
}

func TestCreateBucket_SetsLocationConstraintForOtherRegions(t *testing.T) {
	m := newTestManager(&mockS3Client{
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			if params.CreateBucketConfiguration == nil {
				t.Fatalf("expected CreateBucketConfiguration to be set for non-default region")
			}
			if string(params.CreateBucketConfiguration.LocationConstraint) != testEUWestRegion {
				t.Errorf("expected LocationConstraint eu-west-1, got %q", params.CreateBucketConfiguration.LocationConstraint)
			}
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)
	m.region = testEUWestRegion

	if err := m.CreateBucket(context.Background()); err != nil {
		t.Fatalf("CreateBucket returned error: %v", err)
	}
}

func TestCreateBucket_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			return nil, errors.New("create failed")
		},
	}, nil)

	if err := m.CreateBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CreateBucket, got nil")
	}
}

// --- ensureVersioning ---

func TestEnsureVersioning_Success(t *testing.T) {
	called := false
	m := newTestManager(&mockS3Client{
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			called = true
			if params.VersioningConfiguration.Status != s3types.BucketVersioningStatusEnabled {
				t.Errorf("expected versioning status Enabled, got %q", params.VersioningConfiguration.Status)
			}
			return &s3sdk.PutBucketVersioningOutput{}, nil
		},
	}, nil)

	if err := m.ensureVersioning(context.Background()); err != nil {
		t.Fatalf("ensureVersioning returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected PutBucketVersioning to be called")
	}
}

func TestEnsureVersioning_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			return nil, errors.New("versioning failed")
		},
	}, nil)

	if err := m.ensureVersioning(context.Background()); err == nil {
		t.Fatalf("expected error from ensureVersioning, got nil")
	}
}

// --- ensureLifecyclePolicy ---

func TestEnsureLifecyclePolicy_Success(t *testing.T) {
	called := false
	m := newTestManager(&mockS3Client{
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			called = true
			return &s3sdk.PutBucketLifecycleConfigurationOutput{}, nil
		},
	}, nil)

	if err := m.ensureLifecyclePolicy(context.Background()); err != nil {
		t.Fatalf("ensureLifecyclePolicy returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected PutBucketLifecycleConfiguration to be called")
	}
}

func TestEnsureLifecyclePolicy_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			return nil, errors.New("lifecycle failed")
		},
	}, nil)

	if err := m.ensureLifecyclePolicy(context.Background()); err == nil {
		t.Fatalf("expected error from ensureLifecyclePolicy, got nil")
	}
}

// --- ReconcileAppIRSA ---

func TestReconcileAppIRSA_CreatesRoleWhenNotFound(t *testing.T) {
	createCalled := false
	putPolicyCalled := false
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			createCalled = true
			return &iam.CreateRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			putPolicyCalled = true
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	roleArn, err := m.ReconcileAppIRSA(context.Background())
	if err != nil {
		t.Fatalf("ReconcileAppIRSA returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateRole to be called when role does not exist")
	}
	if !putPolicyCalled {
		t.Fatalf("expected PutRolePolicy to be called")
	}
	if roleArn != testIRSARoleARN {
		t.Errorf("expected role arn to be returned, got %q", roleArn)
	}
}

func TestReconcileAppIRSA_UpdatesTrustPolicyWhenRoleExists(t *testing.T) {
	updateCalled := false
	createCalled := false
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			createCalled = true
			return &iam.CreateRoleOutput{}, nil
		},
		updateAssumeRolePolicyFunc: func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
			updateCalled = true
			return &iam.UpdateAssumeRolePolicyOutput{}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	roleArn, err := m.ReconcileAppIRSA(context.Background())
	if err != nil {
		t.Fatalf("ReconcileAppIRSA returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected CreateRole not to be called when role already exists")
	}
	if !updateCalled {
		t.Fatalf("expected UpdateAssumeRolePolicy to be called when role already exists")
	}
	if roleArn != testIRSARoleARN {
		t.Errorf("expected role arn to be returned, got %q", roleArn)
	}
}

func TestReconcileAppIRSA_PropagatesCreateRoleError(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			return nil, errors.New("create role failed")
		},
	})

	_, err := m.ReconcileAppIRSA(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileAppIRSA, got nil")
	}
}

func TestReconcileAppIRSA_PropagatesPutRolePolicyError(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		updateAssumeRolePolicyFunc: func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
			return &iam.UpdateAssumeRolePolicyOutput{}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return nil, errors.New("put policy failed")
		},
	})

	_, err := m.ReconcileAppIRSA(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileAppIRSA, got nil")
	}
}

// --- ReconcileBucket ---

func TestReconcileBucket_HappyPathReturnsRoleARN(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return matchingTagging(), nil
		},
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			return &s3sdk.PutBucketVersioningOutput{}, nil
		},
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			return &s3sdk.PutBucketLifecycleConfigurationOutput{}, nil
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		updateAssumeRolePolicyFunc: func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
			return &iam.UpdateAssumeRolePolicyOutput{}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.RoleARN != testIRSARoleARN {
		t.Errorf("expected role arn to be returned, got %q", result.RoleARN)
	}
}

func TestReconcileBucket_ShortCircuitsOnBucketError(t *testing.T) {
	iamCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(403, "access denied")
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			iamCalled = true
			return &iam.GetRoleOutput{}, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileBucket, got nil")
	}
	if iamCalled {
		t.Fatalf("expected IRSA reconciliation to be skipped after bucket error")
	}
}

func TestReconcileBucket_ShortCircuitsOnNotOwnedBucket(t *testing.T) {
	iamCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
				},
			}, nil
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			iamCalled = true
			return &iam.GetRoleOutput{}, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned from ReconcileBucket, got %v", err)
	}
	if iamCalled {
		t.Fatalf("expected IRSA reconciliation to be skipped when bucket isn't owned by this Application")
	}
}
