package naming

import (
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testAppName = "demo-app"

func newTestApplication() *forgev1alpha1.Application {
	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: "default"},
	}
}

func TestNames(t *testing.T) {
	app := newTestApplication()

	tests := []struct {
		name     string
		fn       func(*forgev1alpha1.Application) string
		expected string
	}{
		{name: "Service", fn: Service, expected: testAppName},
		{name: "Deployment", fn: Deployment, expected: "demo-app-deployment"},
		{name: "Ingress", fn: Ingress, expected: testAppName},
		{name: "HPA", fn: HPA, expected: "demo-app-hpa"},
		{name: "PDB", fn: PDB, expected: "demo-app-pdb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(app); got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
