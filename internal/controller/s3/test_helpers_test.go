package s3storage

import (
	"context"
	"fmt"
	"net/http"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	testNamespace    = "default"
	testAppName      = "demo-app"
	testBucket       = "demo-bucket"
	testRegion       = "us-east-1"
	testEUWestRegion = "eu-west-1"
	testSecretName   = "aws-creds"
	testIRSARoleARN  = "arn:aws:iam::123456789012:role/app-irsa-demo-app"
	testAppUID       = types.UID("11111111-1111-1111-1111-111111111111")
	testOtherUID     = types.UID("22222222-2222-2222-2222-222222222222")
)

func newTestApp() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace, UID: testAppUID},
	}
}

// newHTTPStatusError builds an error that unwraps to an awshttp.ResponseError
// with the given HTTP status code, mirroring what the AWS SDK returns for
// untyped API errors.
func newHTTPStatusError(statusCode int, message string) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{
				Response: &http.Response{StatusCode: statusCode},
			},
			Err: fmt.Errorf("%s", message),
		},
	}
}

func newTestManager(s3Client S3API, iamClient IAMAPI) *Manager {
	return &Manager{
		s3client:  s3Client,
		iamclient: iamClient,
		app: &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace, UID: testAppUID},
		},
		storage:            &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testRegion},
		bucket:             testBucket,
		region:             testRegion,
		serviceAccountName: "demo-app-sa",
		OIDCProviderARN:    "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
		OIDCProviderURL:    "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
	}
}

type mockS3Client struct {
	headBucketFunc               func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error)
	createBucketFunc             func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error)
	putBucketVersioningFunc      func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error)
	putBucketLifecycleConfigFunc func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error)
	deleteBucketFunc             func(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error)
	listObjectVersionsFunc       func(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error)
	listMultipartUploadsFunc     func(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error)
	abortMultipartUploadFunc     func(ctx context.Context, params *s3sdk.AbortMultipartUploadInput, optFns ...func(*s3sdk.Options)) (*s3sdk.AbortMultipartUploadOutput, error)
	deleteObjectsFunc            func(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error)
	getBucketTaggingFunc         func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error)
	putBucketTaggingFunc         func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error)
}

func (m *mockS3Client) HeadBucket(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
	return m.headBucketFunc(ctx, params, optFns...)
}

func (m *mockS3Client) CreateBucket(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
	return m.createBucketFunc(ctx, params, optFns...)
}

func (m *mockS3Client) PutBucketVersioning(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
	return m.putBucketVersioningFunc(ctx, params, optFns...)
}

func (m *mockS3Client) PutBucketLifecycleConfiguration(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
	return m.putBucketLifecycleConfigFunc(ctx, params, optFns...)
}

func (m *mockS3Client) DeleteBucket(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error) {
	return m.deleteBucketFunc(ctx, params, optFns...)
}

func (m *mockS3Client) ListObjectVersions(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error) {
	return m.listObjectVersionsFunc(ctx, params, optFns...)
}

func (m *mockS3Client) ListMultipartUploads(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error) {
	return m.listMultipartUploadsFunc(ctx, params, optFns...)
}

func (m *mockS3Client) AbortMultipartUpload(ctx context.Context, params *s3sdk.AbortMultipartUploadInput, optFns ...func(*s3sdk.Options)) (*s3sdk.AbortMultipartUploadOutput, error) {
	return m.abortMultipartUploadFunc(ctx, params, optFns...)
}

func (m *mockS3Client) DeleteObjects(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error) {
	return m.deleteObjectsFunc(ctx, params, optFns...)
}

func (m *mockS3Client) GetBucketTagging(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
	return m.getBucketTaggingFunc(ctx, params, optFns...)
}

func (m *mockS3Client) PutBucketTagging(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
	return m.putBucketTaggingFunc(ctx, params, optFns...)
}

type mockIAMClient struct {
	getRoleFunc                func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	createRoleFunc             func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	updateAssumeRolePolicyFunc func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error)
	putRolePolicyFunc          func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	deleteRolePolicyFunc       func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	deleteRoleFunc             func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
}

func (m *mockIAMClient) GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	return m.getRoleFunc(ctx, params, optFns...)
}

func (m *mockIAMClient) CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	return m.createRoleFunc(ctx, params, optFns...)
}

func (m *mockIAMClient) UpdateAssumeRolePolicy(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
	return m.updateAssumeRolePolicyFunc(ctx, params, optFns...)
}

func (m *mockIAMClient) PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	return m.putRolePolicyFunc(ctx, params, optFns...)
}

func (m *mockIAMClient) DeleteRolePolicy(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	return m.deleteRolePolicyFunc(ctx, params, optFns...)
}

func (m *mockIAMClient) DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	return m.deleteRoleFunc(ctx, params, optFns...)
}
