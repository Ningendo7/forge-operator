package akamaiobjstr

import (
	"context"
	"encoding/json"
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
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
)

const ownerMarkerKey = ".forge-operator-owner"

// ownerMarker is the JSON body written to ownerMarkerKey, recording the
// owning Application's UID and namespace (so cross-namespace adoption can be
// evaluated, see naming.EvaluateBucketAdoption) plus its stable storage
// ownership ID (so a Velero restore or cluster migration -- which resets UID
// but not this annotation -- can reclaim automatically, see
// naming.StorageOwnershipIDAnnotation's doc comment). OwnershipID is
// deliberately omitempty: a marker written before that ID existed simply
// won't have one, same treatment as the legacy bare-UID format.
type ownerMarker struct {
	UID         string `json:"uid"`
	Namespace   string `json:"namespace"`
	OwnershipID string `json:"ownershipId,omitempty"`
}

// parseOwnerMarker decodes the ownership marker body. Markers written before
// namespace-scoped ownership shipped are a bare UID string, not JSON --
// legacy reports true for those, and the returned Namespace is always "".
func parseOwnerMarker(body []byte) (owner ownerMarker, legacy bool) {
	if err := json.Unmarshal(body, &owner); err != nil || owner.UID == "" {
		return ownerMarker{UID: string(body)}, true
	}
	return owner, false
}

// bucketCreationClaimWindow bounds how long Created (recorded by
// recordBucketCreated) is trusted as ownership provenance for a bucket found
// with no ownership marker -- long enough to survive a transient failure in
// the marker-write step right after creation, but not an unconditional,
// permanent claim on the name (bucket names are released back to the
// provider's global namespace on deletion, so an unbounded claim would let a
// deleted-and-reused name be silently reclaimed).
const bucketCreationClaimWindow = time.Hour

// akamaiKeyLabelMaxLen is Linode's real Object Storage key label limit
// (not documented in linodego's own types): the API rejects any label
// over 50 characters with "[400] [label] Length must be 3-50 characters".
const akamaiKeyLabelMaxLen = 50

// accessKeyLabel builds this Application's per-app Object Storage access key
// label. Must be used identically everywhere it's referenced (creation in
// ensureAccessKey here, lookup/deletion in cleanup.go): key labels are
// unique per Linode account, not per Kubernetes namespace, so the namespace
// must be folded in or two same-named Applications in different namespaces
// would collide on one shared key.
func (m *Manager) accessKeyLabel() string {
	return naming.CloudResourceName([]string{m.app.Namespace, m.app.Name, "key"}, akamaiKeyLabelMaxLen)
}

// ErrBucketNotOwned means a bucket with the desired name exists but wasn't
// created by this operator for this Application -- surfaced as a Degraded
// condition rather than silently adopted (and later possibly deleted).
var ErrBucketNotOwned = errors.New("bucket already exists and is not owned by forge-operator")

// claimOrVerifyOwnership checks the marker object inside a bucket that
// ensureBucketExists found or created: no marker at all -> claim it by
// writing our UID+namespace+ownership ID; a marker naming a different UID
// -> reclaimed automatically if the marker's stable ownership ID matches
// this Application's own (see naming.StorageOwnershipIDAnnotation's doc
// comment -- this is what makes a Velero restore or cluster migration
// self-heal without a human setting the adopt annotation), otherwise
// naming.EvaluateBucketAdoption decides (existence + namespace scoping); a
// marker matching this Application -> already ours, backfilling the
// namespace/ownership ID if the marker predates either or is legacy
// (bare-UID) format.
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

	myOwnershipID := m.app.Annotations[naming.StorageOwnershipIDAnnotation]

	owner, legacy := parseOwnerMarker(body)
	if owner.UID == string(m.app.UID) {
		if legacy || owner.Namespace != m.app.Namespace || owner.OwnershipID != myOwnershipID {
			// Backfill a marker written before namespace/ownership-ID
			// scoped ownership shipped -- safe, since the UID already
			// confirms this Application owns it.
			return m.claimOwnership(ctx, s3Client)
		}
		return nil
	}

	// A different UID recorded the marker, but this Application's own
	// stable ownership ID matches it -- this isn't a different Application
	// at all, just this same one recreated with a new UID (a Velero restore
	// or cluster migration; see naming.StorageOwnershipIDAnnotation's doc
	// comment). Reclaim automatically: no human adopt-bucket action needed,
	// and no cross-Application ambiguity, since a genuinely different
	// Application was never handed this ID.
	if !legacy && myOwnershipID != "" && owner.OwnershipID == myOwnershipID {
		forgemetrics.StorageOwnershipReclaimedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage)).Inc()
		return m.claimOwnership(ctx, s3Client)
	}

	if !m.adoptBucketRequested() {
		return ErrBucketNotOwned
	}

	ownerNamespace := owner.Namespace
	if legacy {
		ownerNamespace = ""
	}
	if err := naming.EvaluateBucketAdoption(ctx, m.k8sClient, types.UID(owner.UID), ownerNamespace, m.app.Namespace); err != nil {
		return fmt.Errorf("%w: %v", ErrBucketNotOwned, err)
	}

	forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage)).Inc()
	return m.claimOwnership(ctx, s3Client)
}

func (m *Manager) claimOwnership(
	ctx context.Context,
	s3Client s3ObjectAPI,
) error {
	body, err := json.Marshal(ownerMarker{
		UID:         string(m.app.UID),
		Namespace:   m.app.Namespace,
		OwnershipID: m.app.Annotations[naming.StorageOwnershipIDAnnotation],
	})
	if err != nil {
		return fmt.Errorf("failed to encode ownership marker: %w", err)
	}
	_, err = s3Client.PutObject(ctx, &s3sdk.PutObjectInput{
		Bucket: aws.String(m.bucket),
		Key:    aws.String(ownerMarkerKey),
		Body:   strings.NewReader(string(body)),
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

// recordBucketCreated durably records, via the caller-injected recordCreated
// callback, that this operator itself just created this bucket -- invoked
// immediately after CreateObjectStorageBucket succeeds, before ownership
// marking is even attempted, so a transient failure in that later step
// doesn't erase the record.
func (m *Manager) recordBucketCreated(ctx context.Context) error {
	if m.recordCreated == nil {
		return fmt.Errorf("recordCreated callback not configured")
	}
	if err := m.recordCreated(ctx); err != nil {
		return fmt.Errorf("failed to record bucket creation for %s: %w", m.bucket, err)
	}
	return nil
}

// previouslyCreatedByUs reports whether Application.Status durably records
// this operator having created this exact bucket recently enough that
// CreatedAt still falls within bucketCreationClaimWindow.
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

	// If ownership can't be established, the access key just ensured above
	// is useless -- clean it up rather than leaking it (mirrors
	// verifyOwnershipAndEmptyBucket's identical cleanup on the delete path).
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

	// Region, not the deprecated Cluster field, matches how
	// spec.storage.region is documented for this provider.
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
// key. Linode only returns a key's secret once, at creation, so reusing one
// found by label requires recovering its secret elsewhere first: try the
// operator's own output Secret (naming.StorageSecret), and if that has
// nothing either, delete the orphaned key and create a fresh one -- safe,
// since that output Secret is the only place this operator ever exposes it.
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

	// BucketName confines this key to the Application's own bucket rather
	// than every bucket in the account/region; required on Linode's side.
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
// Secret for a previously-recorded Object Storage secret key. Returns "" (not
// an error) if there's nothing there yet -- the expected outcome the first
// time a key is reused before its secret was ever durably recorded.
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
	// A region can span multiple underlying clusters, so
	// "<region>.linodeobjects.com" isn't guaranteed to be the bucket's real
	// endpoint -- prefer the API's own hostname.
	if bucket != nil && bucket.Hostname != "" {
		return bucket.Hostname
	}
	return fmt.Sprintf("%s.linodeobjects.com", m.region)
}
