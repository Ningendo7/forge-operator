package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newTestApplication() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-app",
			Namespace: "default",
		},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}
}

type failingPatchClient struct {
	client.Client
}

func (c *failingPatchClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	return fmt.Errorf("patch failed")
}

type mockS3StorageManager struct {
	reconcileBucketFunc func(ctx context.Context) (*s3storage.StorageResult, error)
}

func (m *mockS3StorageManager) ReconcileBucket(ctx context.Context) (*s3storage.StorageResult, error) {
	return m.reconcileBucketFunc(ctx)
}

type mockAkamaiStorageManager struct {
	reconcileBucketFunc func(ctx context.Context) (*akamaiobjstr.StorageResult, error)
}

func (m *mockAkamaiStorageManager) ReconcileBucket(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
	return m.reconcileBucketFunc(ctx)
}
