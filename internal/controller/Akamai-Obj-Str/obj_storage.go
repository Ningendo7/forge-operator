package akamaiobjstr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3sdktypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/linodego"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

const ownerMarkerKey = ".forge-operator-owner"

// akamaiKeyLabelMaxLen: Linode's Object Storage key label limit isn't
// documented in linodego's own types, so this uses AWS's IAM role name
// limit (64) as a conservative stand-in -- adjust if Linode's actual limit
// turns out to differ.
const akamaiKeyLabelMaxLen = 64

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
			if m.previouslyCreatedByUs() || m.adoptBucketRequested() {
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
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   m.bucket,
		Created:  true,
	}
	if err := m.k8sClient.Status().Update(ctx, m.app); err != nil {
		return fmt.Errorf("failed to record bucket creation for %s: %w", m.bucket, err)
	}
	return nil
}

// previouslyCreatedByUs reports whether Application.Status durably records
// this operator having created this exact bucket in an earlier reconcile.
func (m *Manager) previouslyCreatedByUs() bool {
	return m.app.Status.Storage != nil &&
		m.app.Status.Storage.Bucket == m.bucket &&
		m.app.Status.Storage.Created
}

// ReconcileBucket orchestrates bucket + key setup.
func (m *Manager) ReconcileBucket(
	ctx context.Context,
) (*StorageResult, error) {

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

	endpoint := m.resolveEndpoint(bucket)
	if err := m.claimOrVerifyOwnership(ctx, endpoint, keyResult.AccessKey, keyResult.SecretKey); err != nil {
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

func (m *Manager) ensureAccessKey(
	ctx context.Context,
) (*AccessKeyResult, error) {

	keyLabel := m.accessKeyLabel()

	keys, err := m.akamaiClient.ListObjectStorageKeys(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list storage keys: %w", err)
	}

	// Reuse existing key if it exists
	for _, key := range keys {
		if key.Label == keyLabel {
			return &AccessKeyResult{
				AccessKey: key.AccessKey,
				SecretKey: "",
			}, nil
		}
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
