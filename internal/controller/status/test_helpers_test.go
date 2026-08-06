package status

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newTestApplication() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}
}

func findCondition(app *forgev1alpha1.Application, condType string) *metav1.Condition {
	for i := range app.Status.Conditions {
		if app.Status.Conditions[i].Type == condType {
			return &app.Status.Conditions[i]
		}
	}
	return nil
}

type failingStatusClient struct {
	client.Client
}

func (c *failingStatusClient) Status() client.SubResourceWriter {
	return &failingSubResourceWriter{}
}

type failingSubResourceWriter struct {
	client.SubResourceWriter
}

func (w *failingSubResourceWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return fmt.Errorf("status update failed")
}
