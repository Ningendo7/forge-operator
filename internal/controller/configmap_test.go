package controller

import (
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDesiredConfigMapUsesConfiguredNameAndDefaults(t *testing.T) {
	r := &ApplicationReconciler{}
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	cm := r.desiredConfigMap(app)
	if cm.Name != "demo-app-config" {
		t.Fatalf("expected default config map name demo-app-config, got %q", cm.Name)
	}
	if cm.Data["app-name"] != app.Name {
		t.Fatalf("expected app-name data to be %q, got %q", app.Name, cm.Data["app-name"])
	}
	if cm.Data["image"] != app.Spec.Image {
		t.Fatalf("expected image data to be %q, got %q", app.Spec.Image, cm.Data["image"])
	}

	app.Spec.Container.ConfigMapName = "custom-config"
	cm = r.desiredConfigMap(app)
	if cm.Name != "custom-config" {
		t.Fatalf("expected configured config map name custom-config, got %q", cm.Name)
	}
}
