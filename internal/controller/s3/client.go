package s3storage

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"

	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"golang.org/x/time/rate"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/ratelimit"
)

const defaultRegion = "us-east-1"

// ErrCredentialsSecretNotFound means spec.storage.secretName is set but
// that Secret doesn't exist yet -- distinct from other NewManager errors so
// callers can surface a specific, filterable status reason instead of a
// generic failure.
var ErrCredentialsSecretNotFound = errors.New("credentials secret not found")

// S3API defines the interface for interacting with AWS S3.
type S3API interface {
	CreateBucket(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error)
	HeadBucket(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error)
	PutBucketLifecycleConfiguration(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error)
	DeleteBucketLifecycle(ctx context.Context, params *s3sdk.DeleteBucketLifecycleInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketLifecycleOutput, error)
	DeleteBucket(ctx context.Context, params *s3sdk.DeleteBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteBucketOutput, error)
	ListObjectVersions(ctx context.Context, params *s3sdk.ListObjectVersionsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectVersionsOutput, error)
	ListMultipartUploads(ctx context.Context, params *s3sdk.ListMultipartUploadsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.ListMultipartUploadsOutput, error)
	AbortMultipartUpload(ctx context.Context, params *s3sdk.AbortMultipartUploadInput, optFns ...func(*s3sdk.Options)) (*s3sdk.AbortMultipartUploadOutput, error)
	GetBucketTagging(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error)
	PutBucketTagging(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error)
	DeleteObjects(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error)
}

// IAMAPI defines the interface for interacting with AWS IAM.
type IAMAPI interface {
	GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	UpdateAssumeRolePolicy(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error)
	PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	DeleteRolePolicy(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
}

type StorageResult struct {
	RoleARN string
}

// Manager handles S3 interactions for the Application controller.
type Manager struct {
	k8sClient              client.Client
	s3client               S3API
	iamclient              IAMAPI
	app                    *forgev1alpha1.Application
	storage                *forgev1alpha1.StorageSpec
	bucket                 string
	region                 string
	serviceAccountName     string // Name of the ServiceAccount to be used for IRSA
	OIDCProviderARN        string // EKS OIDC provider ARN needed for trust policy
	OIDCProviderURL        string // EKS OIDC provider (without https://)
	PermissionsBoundaryARN string // AWS IAM policy that caps every app-irsa-* role the operator creates
	recordCreated          func(ctx context.Context) error
}

func NewManager(
	ctx context.Context,
	k8sClient client.Client,
	app *forgev1alpha1.Application,
	serviceAccountName string,
	oidcProviderARN string,
	oidcProviderURL string,
	permissionsBoundaryARN string,
	recordCreated func(ctx context.Context) error,
	s3Limiter *rate.Limiter,
	iamLimiter *rate.Limiter,
) (*Manager, error) {

	storage := app.Spec.Storage
	if storage == nil {
		return nil, fmt.Errorf("storage spec is nil for application %s", app.Name)
	}
	if oidcProviderARN == "" {
		return nil, fmt.Errorf("OIDC_PROVIDER_ARN is not configured, required to create IRSA roles for application %s", app.Name)
	}
	if oidcProviderURL == "" {
		return nil, fmt.Errorf("OIDC_PROVIDER_URL is not configured, required to create IRSA roles for application %s", app.Name)
	}
	if permissionsBoundaryARN == "" {
		return nil, fmt.Errorf("APP_IRSA_PERMISSIONS_BOUNDARY_ARN is not configured, required to create IRSA roles for application %s", app.Name)
	}

	region := storage.Region
	if region == "" {
		region = defaultRegion
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
			if apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("%w: %s", ErrCredentialsSecretNotFound, storage.SecretName)
			}
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

	// Every S3 and IAM call this Manager makes (both clients are built
	// FromConfig below) gets its own span automatically from here on --
	// this middleware is the entire instrumentation for individual AWS API
	// calls; there's no per-call span code anywhere else in this package.
	// Like every span in this operator, these become no-ops if tracing was
	// never initialized (see internal/controller/observability's Init).
	otelaws.AppendMiddlewares(&awsCfg.APIOptions)

	s3client := s3sdk.NewFromConfig(awsCfg, func(o *s3sdk.Options) {
		o.APIOptions = append(o.APIOptions, ratelimit.AWSMiddleware(s3Limiter, "s3"))
		if storage.Endpoint != "" {
			o.BaseEndpoint = aws.String(storage.Endpoint)
			o.UsePathStyle = true // Use path-style addressing for custom endpoints
		}
	})

	// A separate limiter from s3client's, not the shared awsCfg.APIOptions
	// both clients would otherwise inherit -- IAM's real rate limits are
	// far tighter than S3's, and sharing one budget would throttle S3's
	// much larger share of the traffic down to IAM's ceiling for no
	// reason tied to S3's own capacity.
	iamclient := iam.NewFromConfig(awsCfg, func(o *iam.Options) {
		o.APIOptions = append(o.APIOptions, ratelimit.AWSMiddleware(iamLimiter, "iam"))
	})

	return &Manager{
		k8sClient:              k8sClient,
		s3client:               s3client,
		iamclient:              iamclient,
		app:                    app,
		storage:                storage,
		region:                 region,
		bucket:                 storage.Bucket,
		serviceAccountName:     serviceAccountName,
		OIDCProviderARN:        oidcProviderARN,
		OIDCProviderURL:        oidcProviderURL,
		PermissionsBoundaryARN: permissionsBoundaryARN,
		recordCreated:          recordCreated,
	}, nil

}
