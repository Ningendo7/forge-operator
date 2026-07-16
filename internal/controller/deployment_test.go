package controller

import (
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVolumeSettingsUseConfiguredValues(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app"},
	}

	if got := configMapNameFor(app); got != "demo-app-config" {
		t.Fatalf("expected default config map name demo-app-config, got %q", got)
	}

	if got := secretNameFor(app); got != "demo-app-secret" {
		t.Fatalf("expected default secret name demo-app-secret, got %q", got)
	}

	if got := configMountPathFor(app); got != "/etc/demo-app/config" {
		t.Fatalf("expected default config mount path /etc/demo-app/config, got %q", got)
	}

	if got := secretMountPathFor(app); got != "/etc/demo-app/secret" {
		t.Fatalf("expected default secret mount path /etc/demo-app/secret, got %q", got)
	}

	app.Spec.Container.ConfigMapName = "custom-config"
	app.Spec.Container.SecretName = "custom-secret"
	app.Spec.Container.ConfigMountPath = "/custom/config"
	app.Spec.Container.SecretMountPath = "/custom/secret"

	if got := configMapNameFor(app); got != "custom-config" {
		t.Fatalf("expected configured config map name custom-config, got %q", got)
	}

	if got := secretNameFor(app); got != "custom-secret" {
		t.Fatalf("expected configured secret name custom-secret, got %q", got)
	}

	if got := configMountPathFor(app); got != "/custom/config" {
		t.Fatalf("expected configured config mount path /custom/config, got %q", got)
	}

	if got := secretMountPathFor(app); got != "/custom/secret" {
		t.Fatalf("expected configured secret mount path /custom/secret, got %q", got)
	}
}
