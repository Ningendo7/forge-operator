package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// storageReconcileTimeout bounds each storage provisioning attempt (bucket,
// versioning, lifecycle, IAM role/policy, access key -- several sequential
// cloud API calls). Without this, a hung or unusually slow call blocks this
// reconcile indefinitely -- and that blocks every other Application in the
// cluster from reconciling too, not just this one.
// Bounded here means a stuck call becomes a normal, retryable error instead.
const storageReconcileTimeout = 90 * time.Second

// logStorageStatusUpdateError logs a best-effort status write failure without
// masking the underlying storage error that triggered it.
func logStorageStatusUpdateError(ctx context.Context, err error) {
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to update Application storage status")
	}
}

// s3StorageManager and akamaiStorageManager are the minimal surfaces reconcileAWSStorage
// and reconcileAkamaiStorage depend on, so tests can substitute a fake manager instead of
// standing up real AWS/Akamai clients.
type s3StorageManager interface {
	ReconcileBucket(ctx context.Context) (*s3storage.StorageResult, error)
}

type akamaiStorageManager interface {
	ReconcileBucket(ctx context.Context) (*akamaiobjstr.StorageResult, error)
}

// newS3StorageManager and newAkamaiStorageManager are var-bound constructors so tests
// can swap them out; production code always uses the real provider packages.
var newS3StorageManager = func(
	ctx context.Context,
	c client.Client,
	application *forgev1alpha1.Application,
	serviceAccountName string,
	oidcProviderARN string,
	oidcProviderURL string,
) (s3StorageManager, error) {
	return s3storage.NewManager(ctx, c, application, serviceAccountName, oidcProviderARN, oidcProviderURL)
}

var newAkamaiStorageManager = func(
	ctx context.Context,
	c client.Client,
	application *forgev1alpha1.Application,
	defaultRegion string,
) (akamaiStorageManager, error) {
	return akamaiobjstr.NewManager(ctx, c, application, defaultRegion)
}

func (r *ApplicationReconciler) reconcileStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	// If storage spec is nil, clean up any previously-provisioned cloud
	// resource (not just the credentials Secret) before returning --
	// otherwise removing spec.storage from an Application would silently
	// orphan its bucket. finalizeApplication does exactly the cleanup (or
	// deliberate Retain-skip) this needs, sourcing what to clean up from
	// Status.Storage since Spec.Storage is nil here.
	if application.Spec.Storage == nil {
		if application.Status.Storage != nil {
			if err := r.finalizeApplication(ctx, application); err != nil {
				return fmt.Errorf("failed to clean up previously provisioned storage: %w", err)
			}
			// finalizeApplication only ever leaves the StorageReady condition
			// at "cleanup in progress" (or a terminal failure) -- on the real
			// deletion path that's harmless since the whole Application is
			// gone moments later, but here the Application lives on, so a
			// stuck, misleading condition would be left behind permanently.
			// Removing it entirely (rather than setting some other reason)
			// matches the condition an Application that never had storage
			// configured shows: none at all.
			application.Status.Storage = nil
			apimeta.RemoveStatusCondition(&application.Status.Conditions, storagestatus.StorageReady)
			if err := r.Status().Update(ctx, application); err != nil {
				return fmt.Errorf("failed to clear storage status after cleanup: %w", err)
			}
		}
		return r.reconcileStorageSecret(ctx, application, nil)
	}

	// Provision Backend Cloud Storage Resources. akamaiCreds is only ever a
	// local value for the duration of this call: it must never be assigned to
	// application.Status (see the AkamaiStorageStatus doc comment) since that
	// gets persisted as plaintext and is far more widely readable than the
	// storage Secret it ends up in.
	var akamaiCreds *akamaiobjstr.StorageResult
	switch application.Spec.Storage.Provider {
	case forgev1alpha1.ProviderAWSS3:
		if err := r.reconcileAWSStorage(ctx, application); err != nil {
			return fmt.Errorf("failed to reconcile AWS storage: %w", err)
		}
	case forgev1alpha1.ProviderAkamaiObjectStorage:
		creds, err := r.reconcileAkamaiStorage(ctx, application)
		if err != nil {
			return fmt.Errorf("failed to reconcile Akamai storage: %w", err)
		}
		akamaiCreds = creds

	default:
		err := fmt.Errorf("unsupported storage provider: %s", application.Spec.Storage.Provider)
		storagestatus.SetNotReady(application, err)
		logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))
		return err
	}

	// Reconcile Storage Secret
	if err := r.reconcileStorageSecret(ctx, application, akamaiCreds); err != nil {
		return fmt.Errorf("failed to reconcile storage secret: %w", err)
	}

	return nil
}

func (r *ApplicationReconciler) reconcileAWSStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (err error) {

	provider := string(forgev1alpha1.ProviderAWSS3)

	// Child span of whatever's already in ctx -- the "Reconcile" root span
	// from application_controller.go, in the normal reconcile path. Every
	// individual AWS API call underneath this (via the otelaws middleware
	// wired into the s3 package's NewManager) nests under this span in
	// turn, since cloudCtx below is derived from this ctx.
	ctx, span := forgemetrics.Tracer().Start(ctx, "reconcileAWSStorage",
		trace.WithAttributes(
			attribute.String("forge.storage.provider", provider),
			attribute.String("forge.storage.bucket", application.Spec.Storage.Bucket),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	cloudCtx, cancel := context.WithTimeout(ctx, storageReconcileTimeout)
	defer cancel()

	start := time.Now()
	defer func() {
		forgemetrics.StorageReconcileDuration.WithLabelValues(provider).Observe(time.Since(start).Seconds())
	}()

	// Initialize S3 Storage Manager with OIDC info for IRSA role creation

	storageManager, err := newS3StorageManager(
		cloudCtx,
		r.Client,
		application,
		serviceAccountNameFor(application),
		r.OIDCProviderARN,
		r.OIDCProviderURL,
	)

	if err != nil {
		storagestatus.SetNotReady(application, err)
		logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, classifyAWSStorageError(err, outcomeNotOwned)).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return fmt.Errorf("failed to create S3 storage manager: %w", err)
	}

	// Reconcile Bucket and IRSA
	result, err := storageManager.ReconcileBucket(cloudCtx)
	if err != nil {
		outcome := classifyAWSStorageError(err, outcomeNotOwned)
		if outcome == outcomeNotOwned {
			storagestatus.SetNotOwned(application, err)
		} else {
			storagestatus.SetNotReady(application, err)
		}
		logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcome).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return fmt.Errorf("failed to reconcile S3 bucket: %w", err)
	}
	if result.RoleARN != "" {
		if err := r.annotateServiceAccountWithIRSA(ctx, application, result.RoleARN); err != nil {
			return err
		}
	}

	// Structured Status metadata. Created/CreatedAt are carried forward from
	// whatever ReconcileBucket already durably recorded (via
	// recordBucketCreated, on the same application pointer) rather than
	// reconstructed here -- CreatedAt in particular must never be reset to
	// "now" on a routine successful reconcile of an already-owned bucket, or
	// it would defeat the whole point of bucketCreationClaimWindow bounding
	// it in the s3 package.
	createdAt := metav1.Now()
	if application.Status.Storage != nil && !application.Status.Storage.CreatedAt.IsZero() {
		createdAt = application.Status.Storage.CreatedAt
	}
	storageStatus := &forgev1alpha1.StorageStatus{
		Provider:       forgev1alpha1.ProviderAWSS3,
		Bucket:         application.Spec.Storage.Bucket,
		Region:         application.Spec.Storage.Region,
		Created:        true,
		CreatedAt:      createdAt,
		SecretName:     application.Spec.Storage.SecretName,
		DeletionPolicy: application.Spec.Storage.DeletionPolicy,
		AWS: &forgev1alpha1.AWSStorageStatus{
			RoleARN: result.RoleARN,
		},
	}

	storagestatus.SetReady(application, storageStatus, "S3 bucket and IRSA role provisioned")

	if err := r.Status().Update(ctx, application); err != nil {
		return fmt.Errorf("failed to update storage status: %w", err)
	}

	forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeReady).Inc()
	forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(1)

	return nil

}

// reconcileAkamaiStorage provisions the Akamai bucket/access key and returns
// the raw credentials to its caller for use building the storage Secret.
// It deliberately does not persist AccessKey/SecretKey anywhere on
// application.Status: see the AkamaiStorageStatus doc comment for why.
func (r *ApplicationReconciler) reconcileAkamaiStorage(
	ctx context.Context,
	application *forgev1alpha1.Application,
) (result *akamaiobjstr.StorageResult, err error) {

	provider := string(forgev1alpha1.ProviderAkamaiObjectStorage)

	// Same reasoning as reconcileAWSStorage's span.
	ctx, span := forgemetrics.Tracer().Start(ctx, "reconcileAkamaiStorage",
		trace.WithAttributes(
			attribute.String("forge.storage.provider", provider),
			attribute.String("forge.storage.bucket", application.Spec.Storage.Bucket),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	cloudCtx, cancel := context.WithTimeout(ctx, storageReconcileTimeout)
	defer cancel()

	start := time.Now()
	defer func() {
		forgemetrics.StorageReconcileDuration.WithLabelValues(provider).Observe(time.Since(start).Seconds())
	}()

	// Initialize Akamai Storage Manager
	storageManager, err := newAkamaiStorageManager(
		cloudCtx,
		r.Client,
		application,
		r.DefaultAkamaiRegion,
	)
	if err != nil {
		storagestatus.SetNotReady(application, err)
		logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, classifyAkamaiStorageError(err, outcomeNotOwned)).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return nil, fmt.Errorf("failed to create Akamai storage manager: %w", err)
	}

	// Reconcile Bucket and Access Key
	result, err = storageManager.ReconcileBucket(cloudCtx)
	if err != nil {
		outcome := classifyAkamaiStorageError(err, outcomeNotOwned)
		if outcome == outcomeNotOwned {
			storagestatus.SetNotOwned(application, err)
		} else {
			storagestatus.SetNotReady(application, err)
		}
		logStorageStatusUpdateError(ctx, r.Status().Update(ctx, application))
		forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcome).Inc()
		forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(0)
		return nil, fmt.Errorf("failed to reconcile Akamai bucket: %w", err)
	}

	// Akamai only returns the secret key once, at creation. On later
	// reconciles, recover it from the storage Secret this controller
	// previously wrote, rather than caching it on the Application (status
	// is far more widely readable than a Secret).
	if result.SecretKey == "" {
		existing := &corev1.Secret{}
		key := types.NamespacedName{Name: naming.StorageSecret(application), Namespace: application.Namespace}
		if err := r.Get(ctx, key, existing); err == nil {
			result.SecretKey = string(existing.Data["secret_key"])
		}
	}

	// See the identical comment in reconcileAWSStorage: CreatedAt must be
	// carried forward, never regenerated, or bucketCreationClaimWindow's
	// bound becomes meaningless.
	createdAt := metav1.Now()
	if application.Status.Storage != nil && !application.Status.Storage.CreatedAt.IsZero() {
		createdAt = application.Status.Storage.CreatedAt
	}
	storageStatus := &forgev1alpha1.StorageStatus{
		Provider:       forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:         application.Spec.Storage.Bucket,
		Region:         application.Spec.Storage.Region,
		Created:        true,
		CreatedAt:      createdAt,
		SecretName:     application.Spec.Storage.SecretName,
		DeletionPolicy: application.Spec.Storage.DeletionPolicy,
		Akamai:         &forgev1alpha1.AkamaiStorageStatus{Endpoint: result.Endpoint},
	}

	storagestatus.SetReady(application, storageStatus, "Akamai bucket and access key provisioned")

	if err := r.Status().Update(ctx, application); err != nil {
		return nil, fmt.Errorf("failed to update storage status: %w", err)
	}

	forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeReady).Inc()
	forgemetrics.StorageReady.WithLabelValues(application.Namespace, application.Name, provider).Set(1)

	return result, nil
}
