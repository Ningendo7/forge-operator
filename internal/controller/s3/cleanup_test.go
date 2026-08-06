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

// --- deleteAllObjectVersions ---

func TestDeleteAllObjectVersions_DeletesVersionsAndMarkers(t *testing.T) {
	var deletedObjects []s3types.ObjectIdentifier
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{
				Versions: []s3types.ObjectVersion{
					{Key: aws.String("file1.txt"), VersionId: aws.String("v1")},
				},
				DeleteMarkers: []s3types.DeleteMarkerEntry{
					{Key: aws.String("file2.txt"), VersionId: aws.String("v2")},
				},
				IsTruncated: aws.Bool(false),
			}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			deletedObjects = params.Delete.Objects
			return &s3sdk.DeleteObjectsOutput{}, nil
		},
	}, nil)

	if err := m.deleteAllObjectVersions(context.Background()); err != nil {
		t.Fatalf("deleteAllObjectVersions returned error: %v", err)
	}
	if len(deletedObjects) != 2 {
		t.Fatalf("expected 2 objects to be deleted, got %d", len(deletedObjects))
	}
}

func TestDeleteAllObjectVersions_SkipsDeleteCallWhenPageEmpty(t *testing.T) {
	deleteCalled := false
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			deleteCalled = true
			return &s3sdk.DeleteObjectsOutput{}, nil
		},
	}, nil)

	if err := m.deleteAllObjectVersions(context.Background()); err != nil {
		t.Fatalf("deleteAllObjectVersions returned error: %v", err)
	}
	if deleteCalled {
		t.Fatalf("expected DeleteObjects not to be called when there are no objects")
	}
}

func TestDeleteAllObjectVersions_ReturnsNilWhenBucketNotFound(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return nil, &s3types.NoSuchBucket{}
		},
	}, nil)

	if err := m.deleteAllObjectVersions(context.Background()); err != nil {
		t.Fatalf("expected nil error when bucket does not exist, got %v", err)
	}
}

func TestDeleteAllObjectVersions_PropagatesListError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return nil, errors.New("list failed")
		},
	}, nil)

	if err := m.deleteAllObjectVersions(context.Background()); err == nil {
		t.Fatalf("expected error from deleteAllObjectVersions, got nil")
	}
}

func TestDeleteAllObjectVersions_PropagatesPerObjectDeleteErrors(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{
				Versions:    []s3types.ObjectVersion{{Key: aws.String("file1.txt"), VersionId: aws.String("v1")}},
				IsTruncated: aws.Bool(false),
			}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			return &s3sdk.DeleteObjectsOutput{
				Errors: []s3types.Error{{Key: aws.String("file1.txt"), Message: aws.String("access denied")}},
			}, nil
		},
	}, nil)

	if err := m.deleteAllObjectVersions(context.Background()); err == nil {
		t.Fatalf("expected error when per-object delete fails, got nil")
	}
}

func TestDeleteAllObjectVersions_ChunksBatchesOver1000Objects(t *testing.T) {
	versions := make([]s3types.ObjectVersion, 1500)
	for i := range versions {
		key := aws.String("file.txt")
		versions[i] = s3types.ObjectVersion{Key: key, VersionId: aws.String("v")}
	}

	deleteCallCount := 0
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{
				Versions:    versions,
				IsTruncated: aws.Bool(false),
			}, nil
		},
		deleteObjectsFunc: func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
			deleteCallCount++
			if len(params.Delete.Objects) > 1000 {
				t.Errorf("expected batch of at most 1000 objects, got %d", len(params.Delete.Objects))
			}
			return &s3sdk.DeleteObjectsOutput{}, nil
		},
	}, nil)

	if err := m.deleteAllObjectVersions(context.Background()); err != nil {
		t.Fatalf("deleteAllObjectVersions returned error: %v", err)
	}
	if deleteCallCount != 2 {
		t.Fatalf("expected 2 batched DeleteObjects calls for 1500 objects, got %d", deleteCallCount)
	}
}

// --- abortMultipartUploads ---

func TestAbortMultipartUploads_AbortsEachUpload(t *testing.T) {
	abortedKeys := []string{}
	m := newTestManager(&mockS3Client{
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			return &s3sdk.ListMultipartUploadsOutput{
				Uploads: []s3types.MultipartUpload{
					{Key: aws.String("upload1.txt"), UploadId: aws.String("id1")},
					{Key: aws.String("upload2.txt"), UploadId: aws.String("id2")},
				},
			}, nil
		},
		abortMultipartUploadFunc: func(ctx context.Context, params *s3sdk.AbortMultipartUploadInput, optFns ...func(*s3sdk.Options)) (*s3sdk.AbortMultipartUploadOutput, error) {
			abortedKeys = append(abortedKeys, aws.ToString(params.Key))
			return &s3sdk.AbortMultipartUploadOutput{}, nil
		},
	}, nil)

	if err := m.abortMultipartUploads(context.Background()); err != nil {
		t.Fatalf("abortMultipartUploads returned error: %v", err)
	}
	if len(abortedKeys) != 2 {
		t.Fatalf("expected 2 uploads aborted, got %d", len(abortedKeys))
	}
}

func TestAbortMultipartUploads_ReturnsNilWhenBucketNotFound(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			return nil, newHTTPStatusError(404, "not found")
		},
	}, nil)

	if err := m.abortMultipartUploads(context.Background()); err != nil {
		t.Fatalf("expected nil error when bucket does not exist, got %v", err)
	}
}

func TestAbortMultipartUploads_PropagatesListError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			return nil, errors.New("list failed")
		},
	}, nil)

	if err := m.abortMultipartUploads(context.Background()); err == nil {
		t.Fatalf("expected error from abortMultipartUploads, got nil")
	}
}

// --- deleteBucket ---

func TestDeleteBucket_Success(t *testing.T) {
	called := false
	m := newTestManager(&mockS3Client{
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			called = true
			return &s3sdk.DeleteBucketOutput{}, nil
		},
	}, nil)

	if err := m.deleteBucket(context.Background()); err != nil {
		t.Fatalf("deleteBucket returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected DeleteBucket to be called")
	}
}

func TestDeleteBucket_ReturnsNilWhenAlreadyDeleted(t *testing.T) {
	m := newTestManager(&mockS3Client{
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			return nil, &s3types.NoSuchBucket{}
		},
	}, nil)

	if err := m.deleteBucket(context.Background()); err != nil {
		t.Fatalf("expected nil error when bucket already deleted, got %v", err)
	}
}

func TestDeleteBucket_PropagatesOtherErrors(t *testing.T) {
	m := newTestManager(&mockS3Client{
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			return nil, errors.New("delete failed")
		},
	}, nil)

	if err := m.deleteBucket(context.Background()); err == nil {
		t.Fatalf("expected error from deleteBucket, got nil")
	}
}

// --- cleanupAppIRSA ---

func TestCleanupAppIRSA_DeletesPolicyAndRole(t *testing.T) {
	policyDeleted := false
	roleDeleted := false
	m := newTestManager(nil, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			policyDeleted = true
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			roleDeleted = true
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	if err := m.cleanupAppIRSA(context.Background()); err != nil {
		t.Fatalf("cleanupAppIRSA returned error: %v", err)
	}
	if !policyDeleted || !roleDeleted {
		t.Fatalf("expected both policy and role to be deleted, policyDeleted=%v roleDeleted=%v", policyDeleted, roleDeleted)
	}
}

func TestCleanupAppIRSA_IgnoresNoSuchEntityOnPolicyDelete(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	if err := m.cleanupAppIRSA(context.Background()); err != nil {
		t.Fatalf("expected nil error when policy already gone, got %v", err)
	}
}

func TestCleanupAppIRSA_ReturnsNilWhenRoleAlreadyDeleted(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
	})

	if err := m.cleanupAppIRSA(context.Background()); err != nil {
		t.Fatalf("expected nil error when role already deleted, got %v", err)
	}
}

func TestCleanupAppIRSA_PropagatesRoleDeleteError(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return nil, errors.New("delete role failed")
		},
	})

	if err := m.cleanupAppIRSA(context.Background()); err == nil {
		t.Fatalf("expected error from cleanupAppIRSA, got nil")
	}
}

// --- CleanupBucket ---

func TestCleanupBucket_HappyPathCallsAllSteps(t *testing.T) {
	abortCalled := false
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}, nil
		},
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			abortCalled = true
			return &s3sdk.ListMultipartUploadsOutput{}, nil
		},
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			return &s3sdk.DeleteBucketOutput{}, nil
		},
	}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	if err := m.CleanupBucket(context.Background()); err != nil {
		t.Fatalf("CleanupBucket returned error: %v", err)
	}
	if !abortCalled {
		t.Fatalf("expected abortMultipartUploads to be called during CleanupBucket")
	}
}

func TestCleanupBucket_PropagatesIRSACleanupError(t *testing.T) {
	m := newTestManager(&mockS3Client{}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return nil, errors.New("policy delete failed")
		},
	})

	if err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when IRSA cleanup fails, got nil")
	}
}

func TestCleanupBucket_PropagatesObjectDeletionError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return nil, errors.New("list failed")
		},
	}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	if err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when object deletion fails, got nil")
	}
}

func TestCleanupBucket_PropagatesAbortMultipartUploadsError(t *testing.T) {
	deleteBucketCalled := false
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}, nil
		},
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			return nil, errors.New("list multipart uploads failed")
		},
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			deleteBucketCalled = true
			return &s3sdk.DeleteBucketOutput{}, nil
		},
	}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	if err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when aborting multipart uploads fails, got nil")
	}
	if deleteBucketCalled {
		t.Fatalf("expected bucket deletion to be skipped after multipart upload abort error")
	}
}

func TestCleanupBucket_PropagatesBucketDeletionError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}, nil
		},
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			return &s3sdk.ListMultipartUploadsOutput{}, nil
		},
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			return nil, errors.New("delete bucket failed")
		},
	}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	if err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when bucket deletion fails, got nil")
	}
}

// --- isNotFoundError ---

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "typed NoSuchBucket", err: &s3types.NoSuchBucket{}, expected: true},
		{name: "404 response error", err: newHTTPStatusError(404, "not found"), expected: true},
		{name: "403 response error", err: newHTTPStatusError(403, "forbidden"), expected: false},
		{name: "generic error", err: errors.New("boom"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFoundError(tt.err); got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
