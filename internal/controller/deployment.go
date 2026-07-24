package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

func (r *ApplicationReconciler) buildVolumeAndMounts(
	application *forgev1alpha1.Application,
) ([]corev1.Volume, []corev1.VolumeMount) {

	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// ConfigMap Volume only if ConfigMapName is specified
	if application.Spec.Container.ConfigMapName != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "config",
			MountPath: configMountPathFor(application),
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapNameFor(application),
					},
				},
			},
		})
	}

	// Secret Volume only if SecretName is specified
	if application.Spec.Container.SecretName != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "secret",
			MountPath: secretMountPathFor(application),
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretNameFor(application),
				},
			},
		})
	}

	return volumes, volumeMounts
}

func (r *ApplicationReconciler) desiredPodSpec(
	application *forgev1alpha1.Application,
) corev1.PodSpec {

	volumes, volumeMounts := r.buildVolumeAndMounts(application)

	port := int32(8080)
	if application.Spec.Container.Port != 0 {
		port = application.Spec.Container.Port
	}

		podSpec := corev1.PodSpec{

		Containers: []corev1.Container{
			{
				Name:  application.Name,
				Image: application.Spec.Image,
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: port,
					},
				},
				Resources: application.Spec.Resources,
				VolumeMounts: volumeMounts,
			},
		},
		Volumes: volumes,
	}
	if shouldCreateServiceAccount(application) {
		podSpec.ServiceAccountName = serviceAccountNameFor(application)
	}
	return podSpec
}

func (r *ApplicationReconciler) desiredDeployment(
	application *forgev1alpha1.Application,
) *appsv1.Deployment {

	labels := map[string]string{
		"app": application.Name,
	}

	var replicas int32 = 1

	if application.Spec.Replicas != nil {
		replicas = *application.Spec.Replicas
	}

	return &appsv1.Deployment{

		// Needed for Server-Side Apply to work correctly
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
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
				Spec: r.desiredPodSpec(application),
			},
		},
	}

}

func (r *ApplicationReconciler) reconcileDeployment(
	ctx context.Context,
	application *forgev1alpha1.Application,
) error {

	logger := logf.FromContext(ctx)
	logger.Info("Reconciling Deployment via Server-Side Apply")

	desired := r.desiredDeployment(application)

	if err := controllerutil.SetControllerReference(application, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Use Server-Side Apply to create or update the Deployment

	err := r.Patch(
		ctx, 
		desired, 
		client.Apply, 
		client.ForceOwnership, 
		client.FieldOwner("forge-operator"),
	)
	if err != nil {
		logger.Error(err, "failed to apply Deployment", "name", desired.Name)
		return fmt.Errorf("failed to server-side apply Deployment: %w", err)
	}

	logger.Info("Successfully reconciled Deployment", "name", desired.Name)
	return nil

}
