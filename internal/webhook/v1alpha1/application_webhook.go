/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

// log is for logging in this package.
var applicationlog = logf.Log.WithName("application-resource")

// SetupApplicationWebhookWithManager registers the webhook for Application in the manager.
func SetupApplicationWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &forgev1alpha1.Application{}).
		WithValidator(&ApplicationCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&ApplicationCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-forge-ningendo7-github-io-v1alpha1-application,mutating=true,failurePolicy=fail,sideEffects=None,groups=forge.ningendo7.github.io,resources=applications,verbs=create;update,versions=v1alpha1,name=mapplication-v1alpha1.kb.io,admissionReviewVersions=v1

// ApplicationCustomDefaulter sets default values on the Application custom
// resource when it's created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type ApplicationCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Application.
func (d *ApplicationCustomDefaulter) Default(_ context.Context, obj *forgev1alpha1.Application) error {
	applicationlog.Info("Defaulting for Application", "name", obj.GetName())

	if obj.Spec.Storage == nil {
		return nil
	}

	// Only Akamai's spec.storage.secretName is safe to default explicitly
	// here: it purely names the operator's generated output Secret (see
	// naming.StorageSecret). For AWS, spec.storage.secretName is overloaded
	// to also mean "read static AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY from
	// this Secret instead of using IRSA" whenever it's non-empty (see
	// internal/controller/s3/client.go) — defaulting it unconditionally
	// would silently break every pure-IRSA Application by making the S3
	// manager think static credentials were requested.
	if obj.Spec.Storage.Provider != forgev1alpha1.ProviderAkamaiObjectStorage {
		return nil
	}

	if obj.Spec.Storage.SecretName == "" {
		obj.Spec.Storage.SecretName = naming.StorageSecret(obj)
	}

	defaultToken := naming.AkamaiTokenSecret(obj)
	if obj.Spec.Storage.Akamai == nil {
		obj.Spec.Storage.Akamai = &forgev1alpha1.AkamaiStorageSpec{}
	}
	if obj.Spec.Storage.Akamai.AccessKeySecretRef == "" {
		obj.Spec.Storage.Akamai.AccessKeySecretRef = defaultToken
	}

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-forge-ningendo7-github-io-v1alpha1-application,mutating=false,failurePolicy=fail,sideEffects=None,groups=forge.ningendo7.github.io,resources=applications,verbs=create;update,versions=v1alpha1,name=vapplication-v1alpha1.kb.io,admissionReviewVersions=v1

// ApplicationCustomValidator validates the Application resource when it's
// created or updated. Client is used for the live Secret-existence checks
// below, which CEL/kubebuilder validation markers structurally can't do
// (CEL only ever sees the object being validated, never other cluster state).
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ApplicationCustomValidator struct {
	Client client.Client
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Application.
func (v *ApplicationCustomValidator) ValidateCreate(ctx context.Context, obj *forgev1alpha1.Application) (admission.Warnings, error) {
	applicationlog.Info("Validation for Application upon creation", "name", obj.GetName())
	return v.validate(ctx, obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Application.
func (v *ApplicationCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *forgev1alpha1.Application) (admission.Warnings, error) {
	applicationlog.Info("Validation for Application upon update", "name", newObj.GetName())
	return v.validate(ctx, newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Application.
func (v *ApplicationCustomValidator) ValidateDelete(_ context.Context, obj *forgev1alpha1.Application) (admission.Warnings, error) {
	applicationlog.Info("Validation for Application upon deletion", "name", obj.GetName())
	return nil, nil
}

// validate holds the Akamai-specific checks shared by create and update.
// Scoped deliberately to Akamai only — AWS's spec.storage.secretName is
// optional/dual-purpose (see the Defaulter's doc comment above) and isn't
// the collision class this webhook was added to catch.
func (v *ApplicationCustomValidator) validate(ctx context.Context, app *forgev1alpha1.Application) (admission.Warnings, error) {
	if app.Spec.Storage == nil || app.Spec.Storage.Provider != forgev1alpha1.ProviderAkamaiObjectStorage {
		return nil, nil
	}

	outputSecret := naming.StorageSecret(app)
	tokenSecret := naming.AkamaiTokenSecret(app)

	if outputSecret == tokenSecret {
		return nil, fmt.Errorf(
			"spec.storage.secretName (%q) must not be the same Secret as spec.storage.akamai.accessKeySecretRef (%q): "+
				"the operator owns and overwrites the former (including deleting it when the Application is deleted), "+
				"which would corrupt or destroy your Akamai API token Secret",
			outputSecret, tokenSecret)
	}

	secret := &corev1.Secret{}
	err := v.Client.Get(ctx, types.NamespacedName{Name: tokenSecret, Namespace: app.Namespace}, secret)
	switch {
	case apierrors.IsNotFound(err):
		return nil, fmt.Errorf(
			"spec.storage.akamai.accessKeySecretRef Secret %q not found in namespace %q",
			tokenSecret, app.Namespace)
	case err != nil:
		// Don't hard-fail admission on a transient API server error; let it
		// through and surface via the normal reconcile/status path instead.
		return admission.Warnings{fmt.Sprintf("could not verify Akamai credentials Secret %q: %v", tokenSecret, err)}, nil
	case len(secret.Data["apiToken"]) == 0:
		return nil, fmt.Errorf("secret %q (spec.storage.akamai.accessKeySecretRef) is missing required key %q", tokenSecret, "apiToken")
	}

	return nil, nil
}
