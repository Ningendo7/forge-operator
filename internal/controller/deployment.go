package controller

import (
	"context"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func configMapNameFor(application *forgev1alpha1.Application) string {
	if application.Spec.Container.ConfigMapName != "" {
		return application.Spec.Container.ConfigMapName
	}
	return application.Name + "-config"
}

func secretNameFor(application *forgev1alpha1.Application) string {
	if application.Spec.Container.SecretName != "" {
		return application.Spec.Container.SecretName
	}
	return application.Name + "-secret"
}

func configMountPathFor(application *forgev1alpha1.Application) string {
	if application.Spec.Container.ConfigMountPath != "" {
		return application.Spec.Container.ConfigMountPath
	}
	return "/etc/" + application.Name + "/config"
}

func secretMountPathFor(application *forgev1alpha1.Application) string {
	if application.Spec.Container.SecretMountPath != "" {
		return application.Spec.Container.SecretMountPath
	}
	return "/etc/" + application.Name + "/secret"
}

func (r *ApplicationReconciler) desiredDeployment(
	application *forgev1alpha1.Application,
) *appsv1.Deployment {

	labels := map[string]string{
		"app": application.Name,
	}

	var replicas int32

	if application.Spec.Replicas == nil {
		replicas = 1
	} else {
		replicas = *application.Spec.Replicas
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name + "-deployment",
			Namespace: application.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{

					Containers: []corev1.Container{
						{
							Name:  application.Name,
							Image: application.Spec.Image,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: application.Spec.Container.Port,
								},
							},
							Resources: application.Spec.Resources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: configMountPathFor(application),
									ReadOnly:  true,
								},
								{
									Name:      "secret",
									MountPath: secretMountPathFor(application),
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMapNameFor(application),
									},
								},
							},
						},
						{
							Name: "secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: secretNameFor(application),
								},
							},
						},
					},
				},
			},
		},
	}

}

func (r *ApplicationReconciler) getDeployment(
	ctx context.Context,
	key client.ObjectKey,
) (*appsv1.Deployment, error) {

	var existing appsv1.Deployment

	if err := r.Get(ctx, key, &existing); err != nil {
		return nil, err
	}

	return &existing, nil

}

func (r *ApplicationReconciler) reconcileDeployment(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Deployment")
	desired := r.desiredDeployment(application)

	// fetch existing deployment
	existing, err := r.getDeployment(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	})

	// create if not found
	if apierrors.IsNotFound(err) {
		logger.Info("Creating Deployment", "name", desired.Name)

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
	existing.Spec.Selector = desired.Spec.Selector
	existing.Spec.Template.ObjectMeta.Labels = desired.Spec.Template.ObjectMeta.Labels
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
	existing.Spec.Template.Spec.Containers[0].Ports = desired.Spec.Template.Spec.Containers[0].Ports
	existing.Spec.Template.Spec.Containers[0].VolumeMounts = desired.Spec.Template.Spec.Containers[0].VolumeMounts
	existing.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes

	if err := r.Patch(ctx, existing, patch); err != nil {
		logger.Error(err, "failed to patch Deployment", "name", existing.Name)
		return err
	}

	logger.Info("Updated Deployment", "name", existing.Name)
	return nil

}
