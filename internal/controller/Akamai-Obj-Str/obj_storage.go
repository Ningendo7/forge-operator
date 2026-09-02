package akamaiobjstr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3sdktypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/linodego"
)

const ownerMarkerKey = ".forge-operator-owner"

// ErrBucketNotOwned means a bucket with the desired name exists but wasn't
// created by this operator for this Application -- surfaced as a Degraded
// condition rather than silently adopted (and later possibly deleted).
var ErrBucketNotOwned = errors.New("bucket already exists and is not owned by forge-operator")

// s3ObjectAPI is the minimal S3-compatible surface claimOrVerifyOwnership
// and claimOwnership depend on, so tests can substitute a fake client
// instead of standing up a real Linode Object Storage endpoint.
type s3ObjectAPI interface {
	GetObject(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error)
}

// newS3ObjectClient is a var-bound constructor so tests can substitute a
// fake S3-compatible client; production code always builds a real one.
// Path-style addressing is used against the bucket's own resolved cluster
// endpoint (the bucket name already stripped back off its hostname) rather
// than guessing a generic "<region>.linodeobjects.com" endpoint -- Linode
// can return a bucket hostname on a different numbered sub-cluster than the
// account's nominal region cluster (observed live: cluster "us-iad-1"
// registered, but the bucket's actual hostname was on "us-iad-10").
var newS3ObjectClient = func(region, clusterEndpoint, accessKey, secretKey string) s3ObjectAPI {
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}

	return s3sdk.NewFromConfig(cfg, func(o *s3sdk.Options) {
		o.BaseEndpoint = aws.String("https://" + clusterEndpoint)
		o.UsePathStyle = true
	})
}

// s3ClientFor builds an S3-compatible client for this bucket's actual
// Linode Object Storage cluster, using the caller-supplied access/secret
// key.
func (m *Manager) s3ClientFor(bucketHostname, accessKey, secretKey string) s3ObjectAPI {
	clusterEndpoint := strings.TrimPrefix(bucketHostname, m.bucket+".")
	return newS3ObjectClient(m.region, clusterEndpoint, accessKey, secretKey)
}

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
			return m.claimOwnership(ctx, s3Client)
		}
		// A genuine failure (permission denied, network error, ...) must
		// NOT be treated as claimable -- only "no marker at all" is.
		return fmt.Errorf("%w: could not verify ownership marker: %v", ErrBucketNotOwned, err)
	}
	defer out.Body.Close()

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return fmt.Errorf("failed to read ownership marker: %w", err)
	}
	if string(body) != string(m.app.UID) {
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

	return newBucket, nil
}

func (m *Manager) ensureAccessKey(
	ctx context.Context,
) (*AccessKeyResult, error) {

	keyLabel := fmt.Sprintf("%s-key", m.app.Name)

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
