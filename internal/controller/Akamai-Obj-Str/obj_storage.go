package akamaiobjstr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3sdktypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/linodego"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
)

const ownerMarkerKey = ".forge-operator-owner"

// bucketCreationClaimWindow bounds how long Created (recorded by
// recordBucketCreated) is trusted as ownership provenance for a bucket
// found with no ownership marker. It exists to survive a transient failure
// in the marker-write step right after creation -- a retry within minutes,
// not an unconditional, permanent claim on this bucket name. Bucket names
// are released back to the provider's global namespace on deletion, so
// without this bound, a bucket we created, deleted, and later had its name
// reused by a completely unrelated bucket would be silently reclaimed the
// next time this unmarked-bucket path runs.
const bucketCreationClaimWindow = time.Hour

// akamaiKeyLabelMaxLen is Linode's real, confirmed Object Storage key
// label limit -- not documented in linodego's own types, so the original
// value here was a guess (AWS's unrelated IAM role name limit, 64, used as
// a "conservative" stand-in). Confirmed wrong live, via this package's own
// integration test: Linode's API rejects any label over 50 characters
// with "[400] [label] Length must be 3-50 characters". A namespace+name
// combination long enough to need truncation at all was silently building
// an invalid label the whole time this was 64.
const akamaiKeyLabelMaxLen = 50

// accessKeyLabel builds this Application's per-app Object Storage access
// key label. Must be used identically everywhere it's referenced (creation
// in ensureAccessKey here, lookup/deletion in cleanup.go): the Application's
// namespace has to be folded in because key labels are unique per Linode
// account, not per Kubernetes namespace, so two same-named Applications in
// different namespaces would otherwise collide on one shared key -- and
// since ensureAccessKey reuses whichever key it finds by label, the second
// Application to reconcile would be handed the first one's real access key,
// scoped to the first Application's bucket.
func (m *Manager) accessKeyLabel() string {
	return naming.CloudResourceName([]string{m.app.Namespace, m.app.Name, "key"}, akamaiKeyLabelMaxLen)
}

// ErrBucketNotOwned means a bucket with the desired name exists but wasn't
// created by this operator for this Application -- surfaced as a Degraded
// condition rather than silently adopted (and later possibly deleted).
var ErrBucketNotOwned = errors.New("bucket already exists and is not owned by forge-operator")

// claimOrVerifyOwnership checks the marker object inside a bucket that
// ensureBucketExists found or created: no marker at all -> claim it by
// writing our UID; a marker present naming a different Application ->
// ErrBucketNotOwned; a marker matching this Application -> already ours.
//
// Deliberately NOT split into a separate "just created, skip the check"
// path: if the marker write failed transiently right after a real
// CreateObjectStorageBucket in an earlier reconcile, the bucket now exists
// with no marker on it, and only this unified path can recover
func (m *Manager) claimOrVerifyOwnership(
	ctx context.Context,
	bucketHostname, accessKey, secretKey string,
) error {
	s3Client := m.s3ClientFor(bucketHostname, accessKey, secretKey)

	out, err := s3Client.GetObject(ctx, &s3sdk.GetObjectInput{
		Bucket: aws.String(m.bucket),
		Key:    aws.String(ownerMarkerKey),
	})
	if err != nil {
		var noSuchKey *s3sdktypes.NoSuchKey
		if errors.As(err, &noSuchKey) {
			if m.previouslyCreatedByUs() {
				return m.claimOwnership(ctx, s3Client)
			}
			if m.adoptBucketRequested() {
				forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage)).Inc()
				return m.claimOwnership(ctx, s3Client)
			}

			return ErrBucketNotOwned
		}
		// A genuine failure (permission denied, network error, ...) must
		// NOT be treated as claimable -- only "no marker at all" is.
		return fmt.Errorf("%w: could not verify ownership marker: %v", ErrBucketNotOwned, err)
	}
	defer func() { _ = out.Body.Close() }()

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return fmt.Errorf("failed to read ownership marker: %w", err)
	}
	if string(body) != string(m.app.UID) {
		if m.adoptBucketRequested() {
			// adopt-bucket is meant for reclaiming a bucket left behind by
			// an Application that's genuinely gone (typically one retained
			// via deletionPolicy: Retain) -- not for taking a bucket away
			// from an Application that's still alive and using it right
			// now. The marker only ever stores a bare UID, so this is the
			// only way to tell those two cases apart.
			exists, existsErr := naming.ApplicationExistsWithUID(ctx, m.k8sClient, types.UID(body))
			if existsErr != nil {
				return fmt.Errorf("%w: could not confirm the previous owner no longer exists: %v", ErrBucketNotOwned, existsErr)
			}
			if exists {
				return fmt.Errorf("%w: adopt-bucket requested, but the Application that currently owns this bucket still exists", ErrBucketNotOwned)
			}

			forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage)).Inc()
			return m.claimOwnership(ctx, s3Client)
		}

		return ErrBucketNotOwned
	}
	return nil
}

func (m *Manager) claimOwnership(
	ctx context.Context,
	s3Client s3ObjectAPI,
) error {
	_, err := s3Client.PutObject(ctx, &s3sdk.PutObjectInput{
		Bucket: aws.String(m.bucket),
		Key:    aws.String(ownerMarkerKey),
		Body:   strings.NewReader(string(m.app.UID)),
	})
	if err != nil {
		return fmt.Errorf("failed to write ownership marker: %w", err)
	}
	return nil
}

// adoptBucketRequested reports whether the Application has explicitly opted
// in to taking over a bucket owned by a different Application, via
// naming.AdoptBucketAnnotation.
func (m *Manager) adoptBucketRequested() bool {
	return m.app.Annotations[naming.AdoptBucketAnnotation] == naming.AdoptBucketAnnotationValue
}

// recordBucketCreated durably records, in Application.Status, that this
// operator itself just created this bucket -- written immediately after
// CreateObjectStorageBucket succeeds, before ownership marking is even
// attempted, so a transient failure in that later step doesn't erase the
// record.
func (m *Manager) recordBucketCreated(ctx context.Context) error {
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{
		Provider:  forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:    m.bucket,
		Created:   true,
		CreatedAt: metav1.Now(),
	}
	if err := m.k8sClient.Status().Update(ctx, m.app); err != nil {
		return fmt.Errorf("failed to record bucket creation for %s: %w", m.bucket, err)
	}
	return nil
}

// previouslyCreatedByUs reports whether Application.Status durably records
// this operator having created this exact bucket in an earlier reconcile,
// recently enough that CreatedAt still falls within
// bucketCreationClaimWindow. Bounded so this can only ever recover from a
// transient failure in the marker-write step shortly after creation, never
// stand in as a permanent claim on the name -- which a bucket that was
// deleted and later recreated by something else entirely would otherwise
// silently inherit.
func (m *Manager) previouslyCreatedByUs() bool {
	status := m.app.Status.Storage
	return status != nil &&
		status.Bucket == m.bucket &&
		status.Created &&
		time.Since(status.CreatedAt.Time) < bucketCreationClaimWindow
}

// ReconcileBucket orchestrates bucket + key setup.
func (m *Manager) ReconcileBucket(
	ctx context.Context,
) (result *StorageResult, err error) {

	if err := m.validateStorageSpec(); err != nil {
		return nil, fmt.Errorf("invalid storage spec: %w", err)
	}

	bucket, err := m.ensureBucketExists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	keyResult, err := m.ensureAccessKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure access key: %w", err)
	}

	// If ownership ultimately can't be established below, the access key
	// just ensured above is useless -- this Application will never be
	// permitted to use this bucket -- so clean it up rather than leaking
	// it. Confirmed live via this package's own integration test: an
	// Application that attempts to claim an already-owned bucket (and is
	// correctly rejected) still created a real, orphaned access key for
	// itself in the process, since ensureAccessKey necessarily runs before
	// ownership can be checked at all. Mirrors the identical cleanup on
	// the delete path in verifyOwnershipAndEmptyBucket. Scoped to
	// ErrBucketNotOwned specifically: any other error is worth retrying
	// with this same key.
	defer func() {
		if errors.Is(err, ErrBucketNotOwned) {
			if cleanupErr := m.deleteApplicationAccessKey(ctx); cleanupErr != nil {
				logf.FromContext(ctx).Error(cleanupErr, "Failed to clean up access key after ownership could not be established", "bucket", m.bucket)
			}
		}
	}()

	endpoint := m.resolveEndpoint(bucket)
	if err = m.claimOrVerifyOwnership(ctx, endpoint, keyResult.AccessKey, keyResult.SecretKey); err != nil {
		return nil, err
	}

	return &StorageResult{
		AccessKey: keyResult.AccessKey,
		SecretKey: keyResult.SecretKey,
		Endpoint:  m.resolveEndpoint(bucket),
	}, nil
}

func (m *Manager) validateStorageSpec() error {
	if m.storage == nil {
		return fmt.Errorf("storage spec is nil")
	}
	if m.bucket == "" {
		return fmt.Errorf("bucket name is empty")
	}
	if m.region == "" {
		return fmt.Errorf("region/cluster is empty")
	}
	return nil
}

func (m *Manager) ensureBucketExists(
	ctx context.Context,
) (bucket *linodego.ObjectStorageBucket, err error) {

	existing, err := m.akamaiClient.GetObjectStorageBucket(ctx, m.region, m.bucket)
	if err == nil {
		return existing, nil
	}

	if !linodego.IsNotFound(err) {
		return nil, fmt.Errorf("failed to query bucket: %w", err)
	}

	// Bucket does not exist, create it. Cluster is deprecated in linodego in
	// favor of Region (a Cluster value like "us-mia-1" maps to Region
	// "us-mia") -- Region is the modern field and also the one that matches
	// how spec.storage.region is documented for this provider.
	createOpts := linodego.ObjectStorageBucketCreateOptions{
		Label:  m.bucket,
		Region: m.region,
	}

	newBucket, err := m.akamaiClient.CreateObjectStorageBucket(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	if err := m.recordBucketCreated(ctx); err != nil {
		return nil, err
	}

	return newBucket, nil
}

// ensureAccessKey finds or creates this Application's Object Storage access
// key. Linode only ever returns a key's secret once, at creation -- listing
// an existing key never includes it. Reusing an existing key without
// accounting for that would hand back an unusable empty secret to
// claimOrVerifyOwnership moments later in this same ReconcileBucket call --
// and not just on some later, deliberate reconcile: any second call to
// ReconcileBucket for the same Application, for any reason including
// ordinary workqueue retry churn, finds the key an earlier call already
// created and hits this path.
//
// Recovery has two tiers. First, try the operator's own previously-written
// output Secret (naming.StorageSecret) -- the common case, covering any
// reconcile after one that fully succeeded. If that has nothing either (the
// key was created, but an earlier reconcile failed before ever reaching the
// step that writes the output Secret), fall back to deleting the orphaned
// key and creating a fresh one: the output Secret is the only place this
// operator ever exposes an Object Storage secret, so if it was never
// written, nothing could possibly be depending on the old key's
// credentials, and replacing it is safe.
func (m *Manager) ensureAccessKey(
	ctx context.Context,
) (*AccessKeyResult, error) {

	keyLabel := m.accessKeyLabel()

	keys, err := m.akamaiClient.ListObjectStorageKeys(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list storage keys: %w", err)
	}

	for _, key := range keys {
		if key.Label != keyLabel {
			continue
		}
		if secret := m.recoverSecretKey(ctx); secret != "" {
			return &AccessKeyResult{
				AccessKey: key.AccessKey,
				SecretKey: secret,
			}, nil
		}
		if err := m.akamaiClient.DeleteObjectStorageKey(ctx, key.ID); err != nil {
			return nil, fmt.Errorf("failed to delete unusable access key '%d': %w", key.ID, err)
		}
		break
	}

	// Create a new scoped access key. BucketName is what actually confines
	// this key to the Application's own bucket rather than every bucket in
	// the account/region — it's a required field on Linode's side (no
	// omitempty on the wire type), so leaving it unset previously meant this
	// wasn't achieving the least-privilege scoping the BucketAccess field
	// exists for.
	perm := linodego.ObjectStorageKeyBucketAccess{
		Region:      m.region,
		BucketName:  m.bucket,
		Permissions: "read_write",
	}

	createOpts := linodego.ObjectStorageKeyCreateOptions{
		Label:        keyLabel,
		BucketAccess: &[]linodego.ObjectStorageKeyBucketAccess{perm},
	}

	key, err := m.akamaiClient.CreateObjectStorageKey(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage key: %w", err)
	}

	return &AccessKeyResult{
		AccessKey: key.AccessKey,
		SecretKey: key.SecretKey,
	}, nil
}

// recoverSecretKey reads this Application's own operator-managed storage
// Secret for a previously-recorded Object Storage secret key. Returns "" if
// there's nothing there yet -- callers must treat that as "no secret
// available", not an error; it's the expected outcome the first time a key
// is reused before its secret was ever durably recorded anywhere.
func (m *Manager) recoverSecretKey(ctx context.Context) string {
	var secret corev1.Secret
	key := types.NamespacedName{
		Name:      naming.StorageSecret(m.app),
		Namespace: m.app.Namespace,
	}

	if err := m.k8sClient.Get(ctx, key, &secret); err != nil {
		return ""
	}
	return string(secret.Data["secret_key"])
}

func (m *Manager) resolveEndpoint(bucket *linodego.ObjectStorageBucket) string {
	if m.storage.Endpoint != "" {
		return m.storage.Endpoint
	}
	// Prefer the API's own hostname for this bucket: a region can now span
	// multiple underlying clusters, so "<region>.linodeobjects.com" isn't
	// guaranteed to be the bucket's real endpoint the way it was back when
	// region and cluster were the same thing.
	if bucket != nil && bucket.Hostname != "" {
		return bucket.Hostname
	}
	return fmt.Sprintf("%s.linodeobjects.com", m.region)
}
