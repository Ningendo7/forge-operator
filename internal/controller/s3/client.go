package s3storage

import (
	"context"
	"fmt"
	
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"

)

// S3API defines the interface for interacting with AWS S3.
type S3API interface {
	CreateBucket(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error)
	HeadBucket(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error)
	PutBucketLifecycleConfiguration(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error)
	DeleteBucket(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error)
	ListObjectsVersions(ctx context.Context, params *s3sdk.ListObjectsVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectsVersionsOutput, error)
	DeleteObjects(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error)
}

// IAMAPI defines the interface for interacting with AWS IAM.
type IAMAPI interface {
	GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	DeleteRolePolicy(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
}

// Manager is responsible for managing S3 interactions for the Application controller.
type Manager struct {
	k8sClient client.Client
	s3client S3API
	iamclient IAMAPI

	app *forgev1alpha1.Application
	storage *forgev1alpha1.StorageSpec

	bucket string
	region string

	oidcArn string // EKS OIDC provider ARN needed for trust policy
	oidcUrl string // EKS OIDC provider (without https://)
}

func NewManager(
	ctx context.Context, 
	k8sClient client.Client, 
	app *forgev1alpha1.Application,
	oidcArn string,
	oidcUrl string,
) (*Manager, error) {

	storage := app.Spec.Storage
	if storage == nil {
		return nil, fmt.Errorf("storage spec is nil for application %s", app.Name)
	}

	region := storage.Region
	if region == "" {
		region = "us-east-1" // default region if not specified
	}

	cfgOptions := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	// Fetch credentials from Secret if referenced in Spec
	if storage.SecretName != "" {
		var secret corev1.Secret
		secretKey := types.NamespacedName{
			Name:      storage.SecretName,
			Namespace: app.Namespace,
		}
		if err := k8sClient.Get(ctx, secretKey, &secret); err != nil {
			return nil, err
		}

		accessKeyBytes, ok1 := secret.Data["AWS_ACCESS_KEY_ID"]
		secretKeyBytes, ok2 := secret.Data["AWS_SECRET_ACCESS_KEY"]
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("AWS credentials not found in secret %s", storage.SecretName)
		}

		sessionToken := string(secret.Data["AWS_SESSION_TOKEN"]) 

		cfgOptions = append(cfgOptions, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(string(accessKeyBytes), string(secretKeyBytes), sessionToken),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, cfgOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3client := s3sdk.NewFromConfig(awsCfg, func(o *s3sdk.Options) {
		if storage.Endpoint != "" {
			o.BaseEndpoint = aws.String(storage.Endpoint)
			o.UsePathStyle = true // Use path-style addressing for custom endpoints
		}
	})

	iamclient := iam.NewFromConfig(awsCfg)

	return &Manager{
		k8sClient: k8sClient,
		s3client:  s3client,
		iamclient: iamclient,
		app:       app,
		storage:   storage,
		region:    region,
		bucket:    storage.Bucket,
		oidcArn:  oidcArn,
		oidcUrl:  oidcUrl,
	}, nil

}
