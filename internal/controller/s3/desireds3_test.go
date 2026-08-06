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
)

// --- ensureBucketExists ---

func TestEnsureBucketExists_DoesNothingWhenBucketFound(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
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

func TestEnsureBucketExists_CreatesBucketOnTypedNotFound(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, &s3types.NotFound{}
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateBucket to be called when bucket is not found")
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
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateBucket to be called on 404 response error")
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
			if string(params.CreateBucketConfiguration.LocationConstraint) != "eu-west-1" {
				t.Errorf("expected LocationConstraint eu-west-1, got %q", params.CreateBucketConfiguration.LocationConstraint)
			}
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)
	m.region = "eu-west-1"

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
				Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::123456789012:role/app-irsa-demo-app")},
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
	if roleArn != "arn:aws:iam::123456789012:role/app-irsa-demo-app" {
		t.Errorf("expected role arn to be returned, got %q", roleArn)
	}
}

func TestReconcileAppIRSA_UpdatesTrustPolicyWhenRoleExists(t *testing.T) {
	updateCalled := false
	createCalled := false
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::123456789012:role/app-irsa-demo-app")},
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
	if roleArn != "arn:aws:iam::123456789012:role/app-irsa-demo-app" {
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
				Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::123456789012:role/app-irsa-demo-app")},
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
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			return &s3sdk.PutBucketVersioningOutput{}, nil
		},
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			return &s3sdk.PutBucketLifecycleConfigurationOutput{}, nil
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::123456789012:role/app-irsa-demo-app")},
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
	if result.RoleARN != "arn:aws:iam::123456789012:role/app-irsa-demo-app" {
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
