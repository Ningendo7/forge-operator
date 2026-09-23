package akamaiobjstr

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/linode/linodego"

	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	"github.com/Ningendo7/forge-operator/internal/controller/ratelimit"
)

// AKAMAIAPI defines the interface for interacting with Linode Object Storage.
type AKAMAIAPI interface {
	ListObjectStorageBuckets(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageBucket, error)
	GetObjectStorageBucket(ctx context.Context, clusterID string, bucket string) (*linodego.ObjectStorageBucket, error)
	CreateObjectStorageBucket(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error)
	DeleteObjectStorageBucket(ctx context.Context, clusterID string, bucket string) error

	ListObjectStorageKeys(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error)
	CreateObjectStorageKey(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error)
	DeleteObjectStorageKey(ctx context.Context, keyID int) error
}

// Manager handles Akamai/Linode interactions for the Application controller.
type Manager struct {
	k8sClient    client.Client
	akamaiClient AKAMAIAPI

	app     *forgev1alpha1.Application
	storage *forgev1alpha1.StorageSpec

	bucket string
	region string

	// recordCreated persists Application.Status recording that this Manager
	// itself just created the bucket -- injected by the caller (storage.go)
	// rather than written here, so this package never needs to know
	// Application.Status's shape or how it's durably persisted. nil for
	// cleanup-only managers, which never call recordBucketCreated.
	recordCreated func(ctx context.Context) error

	// objectLimiter paces calls to this bucket's own S3-compatible
	// endpoint (marker GetObject/PutObject) -- stored on Manager rather
	// than applied once at construction like s3client/iamclient in the
	// s3 package, because that client is built lazily in s3ClientFor,
	// only once the bucket's real hostname is known.
	objectLimiter *rate.Limiter
}

type StorageResult struct {
	AccessKey string
	SecretKey string
	Endpoint  string
}

type AccessKeyResult struct {
	AccessKey string
	SecretKey string
}

// s3ObjectAPI is the minimal S3-compatible surface claimOrVerifyOwnership
// and claimOwnership depend on, so tests can substitute a fake client
// instead of standing up a real Linode Object Storage endpoint.
type s3ObjectAPI interface {
	GetObject(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3sdk.ListObjectsV2Input, optFns ...func(*s3sdk.Options)) (*s3sdk.ListObjectsV2Output, error)
	DeleteObjects(ctx context.Context, params *s3sdk.DeleteObjectsInput, optFns ...func(*s3sdk.Options)) (*s3sdk.DeleteObjectsOutput, error)
}

// newS3ObjectClient is a var-bound constructor so tests can substitute a
// fake S3-compatible client. Path-style addressing is used against the
// bucket's own resolved cluster endpoint, not a guessed
// "<region>.linodeobjects.com" -- Linode can place a bucket on a different
// numbered sub-cluster than the account's nominal region (e.g. cluster
// "us-iad-1" registered, but the bucket actually lives on "us-iad-10").
var newS3ObjectClient = func(region, clusterEndpoint, accessKey, secretKey string, objectLimiter *rate.Limiter) s3ObjectAPI {
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}

	// Same otelaws middleware as the main s3 package -- the ownership
	// marker's GetObject/PutObject calls are the same aws-sdk-go-v2 S3
	// client type, just pointed at Akamai's S3-compatible endpoint instead
	// of AWS's.
	otelaws.AppendMiddlewares(&cfg.APIOptions)

	return s3sdk.NewFromConfig(cfg, func(o *s3sdk.Options) {
		o.APIOptions = append(o.APIOptions, ratelimit.AWSMiddleware(objectLimiter, "akamai_object"))
		o.BaseEndpoint = aws.String("https://" + clusterEndpoint)
		o.UsePathStyle = true

		// aws-sdk-go-v2 defaults to computing a flexible checksum (CRC32) on
		// every PutObject/GetObject call. AWS handles this fine, but
		// Akamai/Linode's Ceph RGW-based Object Storage silently accepts
		// the request and never persists the body -- no error, just data
		// loss. WhenRequired restores the classic behavior (checksum only
		// when the operation actually demands one).
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
}

// s3ClientFor builds an S3-compatible client for this bucket's actual
// Linode Object Storage cluster, using the caller-supplied access/secret
// key.
func (m *Manager) s3ClientFor(bucketHostname, accessKey, secretKey string) s3ObjectAPI {
	clusterEndpoint := strings.TrimPrefix(bucketHostname, m.bucket+".")
	return newS3ObjectClient(m.region, clusterEndpoint, accessKey, secretKey, m.objectLimiter)
}

// ErrTokenSecretNotFound means the Akamai API token Secret
// (spec.storage.akamai.accessKeySecretRef) doesn't exist yet -- distinct
// from other NewManager errors so callers can surface a specific,
// filterable status reason instead of a generic failure.
var ErrTokenSecretNotFound = errors.New("token secret not found")

// NewManager creates a new Manager instance for managing Akamai interactions.
// defaultRegion is used only when spec.storage.region is unset -- the
// operator's own DEFAULT_AKAMAI_REGION (see cmd/main.go), never a hardcoded
// fallback baked into this package, since that would only ever be correct
// for one deployment. If both are unset, region ends up empty and Linode's
// API rejects the request with a clear error rather than silently guessing.
func NewManager(
	ctx context.Context,
	k8sClient client.Client,
	app *forgev1alpha1.Application,
	defaultRegion string,
	recordCreated func(ctx context.Context) error,
	accountLimiter *rate.Limiter,
	objectLimiter *rate.Limiter,
) (*Manager, error) {

	storage := app.Spec.Storage
	if storage == nil {
		return nil, fmt.Errorf("storage spec is nil for application %s", app.Name)
	}

	bucket := storage.Bucket
	region := storage.Region
	if region == "" {
		region = defaultRegion
	}

	// A deliberately different Secret than naming.StorageSecret: that one is
	// the operator's own generated, owned-and-overwritten output. Reusing
	// its name for this user-supplied input token would apply the same
	// SSA/garbage-collection lifecycle to a Secret the user manages.
	secretName := naming.AkamaiTokenSecret(app)

	var secret corev1.Secret
	secretKey := types.NamespacedName{
		Name:      secretName,
		Namespace: app.Namespace,
	}

	if err := k8sClient.Get(ctx, secretKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrTokenSecretNotFound, secretName)
		}
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	tokenBytes, ok := secret.Data["apiToken"]
	if !ok || len(tokenBytes) == 0 {
		return nil, fmt.Errorf("key 'apiToken' not found in secret %s", secretName)
	}

	// linodego has no OTel middleware of its own, but accepts a plain
	// *http.Client -- wrapping its Transport with otelhttp.NewTransport
	// instruments every call the same way otelaws covers AWS. accountLimiter
	// is separate from objectLimiter for the same reason s3/client.go splits
	// s3client and iamclient: different infrastructure, different capacity.
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: string(tokenBytes)})
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	oauthClient.Transport = otelhttp.NewTransport(ratelimit.NewRoundTripper(accountLimiter, oauthClient.Transport, "akamai_account"))
	linodeClient := linodego.NewClient(oauthClient)

	return &Manager{
		k8sClient:     k8sClient,
		akamaiClient:  &linodeClient,
		app:           app,
		storage:       storage,
		bucket:        bucket,
		region:        region,
		recordCreated: recordCreated,
		objectLimiter: objectLimiter,
	}, nil
}
