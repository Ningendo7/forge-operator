package akamaiobjstr

import (
	"context"
	"fmt"

	"github.com/linode/linodego"
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

)

// AKAMAIAPI defines the interface for interacting with Akamai / Linode
type AKAMAIAPI interface {
	GetObjectStorageBuckets(ctx context.Context, region, label string) (*linodego.ObjectStorageBucket, error)
	CreateObjectStorageBucket(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error)
	DeleteObjectStorageBucket(ctx context.Context, region, label string) error
	ListObjectStorageKeys(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error)
	CreateObjectStorageKey(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error)
	DeleteObjectStorageKey(ctx context.Context, keyID int) error
}

// Manager is responsible for managing Akamai interactions for the Application controller.
type Manager struct {
	k8sClient client.Client
	akamaiClient AKAMAIAPI

	app *forgev1alpha1.Application
	storage *forgev1alpha1.StorageSpec

	bucket string
	region string
}

// NewManager creates a new Manager instance for managing Akamai interactions.
func NewManager(
	ctx context.Context,
	k8sClient client.Client,
	app *forgev1alpha1.Application,
) (*Manager, error) {

	storage := app.Spec.Storage
	if storage == nil {
		return nil, fmt.Errorf("storage spec is nil for application %s", app.Name)
	}

	bucket := storage.Bucket
	region := storage.Region
	if region == "" {
		region = "us-east-1" // Default region if not specified
	}

	secretName := storage.SecretName
	if secretName == "" {
		secretName = app.Name + "-storage"
	}

	var secret corev1.Secret
	secretKey := types.NamespacedName{
		Name: secretName, 
		Namespace: app.Namespace,
	}

	if err := k8sClient.Get(ctx, secretKey, &secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	tokenBytes, ok := secret.Data["apiToken"]
	if !ok || len(tokenBytes) == 0 {
		return nil, fmt.Errorf("Key 'apiToken' not found in secret %s", secretName)
	}

	// Initialize linode / akamai client
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