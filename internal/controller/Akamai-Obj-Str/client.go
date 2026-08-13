package akamaiobjstr

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	"github.com/linode/linodego"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// NewManager creates a new Manager instance for managing Akamai interactions.
// defaultRegion is used only when the Application doesn't set
// spec.storage.region itself; it's the operator's own DEFAULT_AKAMAI_REGION
// configuration (see cmd/main.go), not a value guessed here. There's no
// further hardcoded fallback: this repo's own Terraform provisions its LKE
// cluster in a specific region, and a literal baked into this package would
// only ever be correct for that one deployment. If both are unset, region
// ends up empty and Linode's API rejects the request with a clear error,
// which is preferable to silently defaulting to a region that may not match
// wherever the operator is actually running.
func NewManager(
	ctx context.Context,
	k8sClient client.Client,
	app *forgev1alpha1.Application,
	defaultRegion string,
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

	// naming.AkamaiTokenSecret is deliberately a different Secret (and
	// different default name) than naming.StorageSecret: that one is the
	// operator's own generated output (bucket access/secret key), owned and
	// overwritten by the controller. Reusing its name here for the
	// user-supplied input token would mean the operator's SSA-applied output
	// fields and the controller-owned lifecycle (SetControllerReference, so
	// the Secret gets garbage collected with the Application) would apply to
	// the user's manually-created token Secret too.
	secretName := naming.AkamaiTokenSecret(app)

	var secret corev1.Secret
	secretKey := types.NamespacedName{
		Name:      secretName,
		Namespace: app.Namespace,
	}

	if err := k8sClient.Get(ctx, secretKey, &secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	tokenBytes, ok := secret.Data["apiToken"]
	if !ok || len(tokenBytes) == 0 {
		return nil, fmt.Errorf("key 'apiToken' not found in secret %s", secretName)
	}

	// Initialize linode client
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: string(tokenBytes)})
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	linodeClient := linodego.NewClient(oauthClient)

	return &Manager{
		k8sClient:    k8sClient,
		akamaiClient: &linodeClient,
		app:          app,
		storage:      storage,
		bucket:       bucket,
		region:       region,
	}, nil
}
