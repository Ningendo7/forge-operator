package controller 

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

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