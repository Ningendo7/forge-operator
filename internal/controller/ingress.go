package controller

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ApplicationReconciler) desiredIngress(application *forgev1alpha1.Application) *networkingv1.Ingress {
	labels := map[string]string{"app": application.Name}

	pathType := networkingv1.PathTypePrefix
	path := "/"
	if application.Spec.Ingress != nil {
		if application.Spec.Ingress.Path != "" {
			path = application.Spec.Ingress.Path
		}
		if application.Spec.Ingress.PathType != nil {
			pathType = *application.Spec.Ingress.PathType
		}
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name,
			Namespace: application.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				Host: "",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     path,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: application.Name,
									Port: networkingv1.ServiceBackendPort{Number: 80},
								},
							},
						}},
					},
				},
			}},
		},
	}

	if application.Spec.Ingress != nil {
		if application.Spec.Ingress.Host != "" {
			ingress.Spec.Rules[0].Host = application.Spec.Ingress.Host
		}
		if application.Spec.Ingress.ClassName != nil {
			ingress.Spec.IngressClassName = application.Spec.Ingress.ClassName
		}
		if len(application.Spec.Ingress.Annotations) > 0 {
			ingress.Annotations = application.Spec.Ingress.Annotations
		}
	}

	return ingress
}

func (r *ApplicationReconciler) getIngress(ctx context.Context, key client.ObjectKey) (*networkingv1.Ingress, error) {
	var existing networkingv1.Ingress
	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *ApplicationReconciler) reconcileIngress(ctx context.Context, application *forgev1alpha1.Application) error {
	if application.Spec.Ingress == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Ingress")

	desired := r.desiredIngress(application)
	existing, err := r.getIngress(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace})
	if apierrors.IsNotFound(err) {
		logger.Info("Creating Ingress", "name", desired.Name)
		if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	} else if err != nil {
		return err
	}

	if !metav1.IsControlledBy(existing, application) {
		if err := controllerutil.SetControllerReference(application, existing, r.Scheme); err != nil {
			return err
		}
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec = desired.Spec

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch Ingress", "name", existing.Name)
		return err
	}

	logger.Info("Updated Ingress", "name", existing.Name)
	return nil
}


func (r *ApplicationReconciler) desiredStorage(application *forgev1alpha1.Application) *corev1.Secret {
	if application.Spec.Storage == nil {
		return nil
	}

	name := application.Name + "-storage"
	if application.Spec.Storage.SecretName != "" {
		name = application.Spec.Storage.SecretName
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: application.Namespace,
			Labels: map[string]string{"app": application.Name},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"provider": application.Spec.Storage.Provider,
			"bucket":   application.Spec.Storage.Bucket,
			"region":   application.Spec.Storage.Region,
			"endpoint": application.Spec.Storage.Endpoint,
		},
	}
}

func (r *ApplicationReconciler) reconcileStorage(ctx context.Context, application *forgev1alpha1.Application) error {
	if application.Spec.Storage == nil {
		return nil
	}

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Storage Secret")

	desired := r.desiredStorage(application)
	if desired == nil {
		return nil
	}

	existing, err := r.getSecret(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace})
	if apierrors.IsNotFound(err) {
		logger.Info("Creating Storage Secret", "name", desired.Name)
		if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	} else if err != nil {
		return err
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.StringData = desired.StringData
	existing.Type = desired.Type

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch Storage Secret", "name", existing.Name)
		return err
	}

	logger.Info("Updated Storage Secret", "name", existing.Name)
	return nil
}

func (r *ApplicationReconciler) reconcileStorageSecret(ctx context.Context, application *forgev1alpha1.Application) error {
	return r.reconcileStorage(ctx, application)
}

func (r *ApplicationReconciler) reconcileObjectStorage(ctx context.Context, application *forgev1alpha1.Application) error {
	return r.reconcileStorage(ctx, application)
}

func (r *ApplicationReconciler) reconcileStorageConfig(ctx context.Context, application *forgev1alpha1.Application) error {
	return r.reconcileStorage(ctx, application)
}

func (r *ApplicationReconciler) reconcileStorageSecretInline(ctx context.Context, application *forgev1alpha1.Application) error {
	return r.reconcileStorage(ctx, application)
}

func (r *ApplicationReconciler) reconcileStorageSecretManager(ctx context.Context, application *forgev1alpha1.Application) error {
	return r.reconcileStorage(ctx, application)
}
