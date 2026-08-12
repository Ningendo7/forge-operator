package controller

import (
	"context"
	"fmt"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

// defaultPodSecurityContext applies when unspecified, for restricted-profile compliance.
func defaultPodSecurityContext() *corev1.PodSecurityContext {
	runAsNonRoot := true
	return &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRoot,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// defaultContainerSecurityContext applies when unspecified. ReadOnlyRootFilesystem is left
// unset since forcing it can break images not built to expect it.
func defaultContainerSecurityContext() *corev1.SecurityContext {
	runAsNonRoot := true
	allowPrivilegeEscalation := false
	return &corev1.SecurityContext{
		RunAsNonRoot:             &runAsNonRoot,
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func podSecurityContextFor(application *forgev1alpha1.Application) *corev1.PodSecurityContext {
	if application.Spec.PodSecurityContext != nil {
		return application.Spec.PodSecurityContext
	}
	return defaultPodSecurityContext()
}

func containerSecurityContextFor(application *forgev1alpha1.Application) *corev1.SecurityContext {
	if application.Spec.Container.SecurityContext != nil {
		return application.Spec.Container.SecurityContext
	}
	return defaultContainerSecurityContext()
}

// defaultResources applies when neither requests nor limits are specified.
func defaultResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
}

// resourcesFor defaults only when both requests and limits are empty; a partial
// spec is respected as-is rather than merged with defaults.
func resourcesFor(application *forgev1alpha1.Application) corev1.ResourceRequirements {
	res := application.Spec.Resources
	if len(res.Requests) == 0 && len(res.Limits) == 0 {
		return defaultResources()
	}
	return res
}

// defaultLivenessProbe uses a TCP check on the container port; guessing an HTTP
// path could turn the safety net into a CrashLoopBackOff.
func defaultLivenessProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       15,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
}

func defaultReadinessProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		TimeoutSeconds:      3,
		FailureThreshold:    3,
	}
}

func livenessProbeFor(application *forgev1alpha1.Application, port int32) *corev1.Probe {
	if application.Spec.Container.LivenessProbe != nil {
		return application.Spec.Container.LivenessProbe
	}
	return defaultLivenessProbe(port)
}

func readinessProbeFor(application *forgev1alpha1.Application, port int32) *corev1.Probe {
	if application.Spec.Container.ReadinessProbe != nil {
		return application.Spec.Container.ReadinessProbe
	}
	return defaultReadinessProbe(port)
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
		SecurityContext: podSecurityContextFor(application),
		Containers: []corev1.Container{
			{
				Name:  application.Name,
				Image: application.Spec.Image,
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: port,
					},
				},
				Env:             application.Spec.Env,
				Resources:       resourcesFor(application),
				VolumeMounts:    volumeMounts,
				SecurityContext: containerSecurityContextFor(application),
				LivenessProbe:   livenessProbeFor(application, port),
				ReadinessProbe:  readinessProbeFor(application, port),
				StartupProbe:    application.Spec.Container.StartupProbe,
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
		appLabelKey: application.Name,
	}

	var replicas int32 = 1

	if application.Spec.Replicas != nil {
		replicas = *application.Spec.Replicas
	}

	return &appsv1.Deployment{

		// Needed for Server-Side Apply to work correctly
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       deploymentKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.Deployment(application),
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
		client.Apply, //nolint:staticcheck // SSA patch via client.Apply is the standard controller-runtime pattern
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
