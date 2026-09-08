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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
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

// matchingBucketTag is the getBucketTaggingFunc used by every CleanupBucket
// test below that isn't itself testing the ownership check -- it lets
// verifyOwnership pass so the rest of CleanupBucket's steps are reachable.
func matchingBucketTag(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
	return &s3sdk.GetBucketTaggingOutput{
		TagSet: []s3types.Tag{{Key: aws.String(ownerTagKey), Value: aws.String(string(testAppUID))}},
	}, nil
}

// --- CleanupBucket ---

func TestCleanupBucket_HappyPathCallsAllSteps(t *testing.T) {
	abortCalled := false
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: matchingBucketTag,
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

	if _, err := m.CleanupBucket(context.Background()); err != nil {
		t.Fatalf("CleanupBucket returned error: %v", err)
	}
	if !abortCalled {
		t.Fatalf("expected abortMultipartUploads to be called during CleanupBucket")
	}
}

func TestCleanupBucket_StillDeletesBucketWhenIRSACleanupFails(t *testing.T) {
	deleteBucketCalled := false
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: matchingBucketTag,
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			return &s3sdk.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}, nil
		},
		listMultipartUploadsFunc: func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
			return &s3sdk.ListMultipartUploadsOutput{}, nil
		},
		deleteBucketFunc: func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
			deleteBucketCalled = true
			return &s3sdk.DeleteBucketOutput{}, nil
		},
	}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			return nil, errors.New("policy delete failed")
		},
	})

	// Mirrors the identical accessKeyErr/err split on the Akamai package's
	// DeleteBucket: the bucket is the billed resource, the role is not, so
	// guaranteeing the costly one gets deleted takes priority over a
	// transient failure cleaning up the free one.
	irsaErr, err := m.CleanupBucket(context.Background())
	if err != nil {
		t.Fatalf("expected CleanupBucket's main error to be nil when only IRSA cleanup fails, got %v", err)
	}
	if irsaErr == nil {
		t.Fatalf("expected CleanupBucket to surface the IRSA cleanup failure separately, got nil")
	}
	if !deleteBucketCalled {
		t.Fatalf("expected the bucket to still be deleted despite the IRSA cleanup failure")
	}
}

func TestCleanupBucket_PropagatesObjectDeletionError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: matchingBucketTag,
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

	if _, err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when object deletion fails, got nil")
	}
}

func TestCleanupBucket_PropagatesAbortMultipartUploadsError(t *testing.T) {
	deleteBucketCalled := false
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: matchingBucketTag,
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

	if _, err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when aborting multipart uploads fails, got nil")
	}
	if deleteBucketCalled {
		t.Fatalf("expected bucket deletion to be skipped after multipart upload abort error")
	}
}

func TestCleanupBucket_PropagatesBucketDeletionError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: matchingBucketTag,
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

	if _, err := m.CleanupBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CleanupBucket when bucket deletion fails, got nil")
	}
}

// --- verifyOwnership / CleanupBucket ownership gating ---

func TestVerifyOwnership_PassesWhenTagMatches(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: matchingBucketTag,
	}, nil)

	if err := m.verifyOwnership(context.Background()); err != nil {
		t.Fatalf("expected nil error when tag matches, got %v", err)
	}
}

func TestVerifyOwnership_PassesWhenBucketAlreadyGone(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchBucketErrorCode}
		},
	}, nil)

	// Confirmed live: without this check, retrying cleanup on a bucket a
	// previous attempt had already successfully deleted (a transient
	// error removing the finalizer itself, a controller restart
	// mid-flight, anything that causes a second CleanupBucket call after
	// the first one actually succeeded) wrongly reported ErrBucketNotOwned
	// for a bucket that was, in fact, already correctly cleaned up --
	// permanently stuck-failing that Application's deletion for no real
	// reason. Distinct from noSuchTagSetErrorCode, which means the bucket
	// still exists but carries no tags.
	if err := m.verifyOwnership(context.Background()); err != nil {
		t.Fatalf("expected nil error when the bucket is already gone, got %v", err)
	}
}

func TestVerifyOwnership_RejectsMismatchedTagRegardlessOfAdoptAnnotation(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))}},
			}, nil
		},
	}, nil)
	// Deliberately different from claimOrVerifyOwnership's create-time
	// behavior: the adopt-bucket annotation lets a mismatched tag be
	// overwritten on the create path, but must NOT unlock deletion of a
	// bucket that, as far as this check can tell, still belongs to someone
	// else -- adoption is supposed to happen via a real, successful
	// reconcile (which rewrites the tag), not be inferred from the
	// annotation alone at the moment of deletion.
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	err := m.verifyOwnership(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned even with the adopt annotation set, got %v", err)
	}
}

func TestVerifyOwnership_RejectsNoSuchTagSetWithNoProvenance(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode}
		},
	}, nil)

	err := m.verifyOwnership(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned when no tag and no provenance, got %v", err)
	}
}

func TestVerifyOwnership_PassesNoSuchTagSetWhenPreviouslyCreatedByUs(t *testing.T) {
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode}
		},
	}, nil)
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: metav1.Now()}

	if err := m.verifyOwnership(context.Background()); err != nil {
		t.Fatalf("expected nil error when previously created by us, got %v", err)
	}
}

func TestCleanupBucket_StillCleansUpOwnIRSARoleWhenBucketNotOwned(t *testing.T) {
	irsaCalled := false
	bucketDeletionCalled := false
	m := newTestManager(&mockS3Client{
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))}},
			}, nil
		},
		listObjectVersionsFunc: func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
			bucketDeletionCalled = true
			return &s3sdk.ListObjectVersionsOutput{}, nil
		},
	}, &mockIAMClient{
		deleteRolePolicyFunc: func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
			irsaCalled = true
			return &iam.DeleteRolePolicyOutput{}, nil
		},
		deleteRoleFunc: func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
			return &iam.DeleteRoleOutput{}, nil
		},
	})

	_, err := m.CleanupBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned from CleanupBucket, got %v", err)
	}
	// The bucket itself must never be touched on uncertain ownership --
	// that part of the original behavior is unchanged.
	if bucketDeletionCalled {
		t.Fatalf("expected bucket content/deletion steps not to run when ownership can't be confirmed")
	}
	// But this Application's own IAM role is unambiguous regardless of
	// what happened to the bucket -- confirmed live as a real leak
	// otherwise: once a bucket is adopted away, ownership verification
	// will permanently fail for this Application, which must not also
	// permanently strand its own role.
	if !irsaCalled {
		t.Fatalf("expected IRSA cleanup to still run even when bucket ownership can't be confirmed")
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
