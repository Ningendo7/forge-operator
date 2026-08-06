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

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

// These specs run the real ApplicationReconciler against a live envtest API
// server, exercising the full watch/reconcile loop rather than calling
// reconcile functions directly. They cover only the core Kubernetes
// resources (Deployment/Service/ConfigMap/Secret/Ingress/HPA/PDB); the
// AWS/Akamai storage paths are out of scope since they require real or
// simulated cloud endpoints.
var _ = Describe("Application integration", func() {

	const namespace = "default"
	const eventualTimeout = 10 * time.Second
	const pollInterval = 200 * time.Millisecond

	It("creates the core child resources for a minimal Application", func() {
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "core-app", Namespace: namespace},
			Spec:       forgev1alpha1.ApplicationSpec{Image: "nginx:latest"},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, app)
		})

		dep := &appsv1.Deployment{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "core-app-deployment", Namespace: namespace}, dep)
		}, eventualTimeout, pollInterval).Should(Succeed())
		Expect(dep.OwnerReferences).To(HaveLen(1))
		Expect(dep.OwnerReferences[0].Name).To(Equal("core-app"))

		svc := &corev1.Service{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "core-app", Namespace: namespace}, svc)
		}, eventualTimeout, pollInterval).Should(Succeed())
		Expect(svc.OwnerReferences).To(HaveLen(1))
	})

	It("becomes Ready once the Deployment reports healthy replicas", func() {
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "ready-app", Namespace: namespace},
			Spec:       forgev1alpha1.ApplicationSpec{Image: "nginx:latest"},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, app)
		})

		dep := &appsv1.Deployment{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "ready-app-deployment", Namespace: namespace}, dep)
		}, eventualTimeout, pollInterval).Should(Succeed())

		// envtest runs no kubelet or replicaset controller, so a healthy
		// rollout has to be simulated by writing the Deployment status directly.
		dep.Status.ObservedGeneration = dep.Generation
		dep.Status.Replicas = 1
		dep.Status.UpdatedReplicas = 1
		dep.Status.AvailableReplicas = 1
		dep.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

		Eventually(func() string {
			got := &forgev1alpha1.Application{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "ready-app", Namespace: namespace}, got); err != nil {
				return ""
			}
			for _, c := range got.Status.Conditions {
				if c.Type == "Ready" {
					return string(c.Status)
				}
			}
			return ""
		}, eventualTimeout, pollInterval).Should(Equal("True"))
	})

	It("creates an HPA when autoscaling is enabled and removes it when disabled", func() {
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "hpa-app", Namespace: namespace},
			Spec: forgev1alpha1.ApplicationSpec{
				Image:       "nginx:latest",
				Autoscaling: &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, app)
		})

		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "hpa-app-hpa", Namespace: namespace}, &autoscalingv2.HorizontalPodAutoscaler{})
		}, eventualTimeout, pollInterval).Should(Succeed())

		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "hpa-app", Namespace: namespace}, app); err != nil {
				return err
			}
			app.Spec.Autoscaling = nil
			return k8sClient.Update(ctx, app)
		}, eventualTimeout, pollInterval).Should(Succeed())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "hpa-app-hpa", Namespace: namespace}, &autoscalingv2.HorizontalPodAutoscaler{})
			return apierrors.IsNotFound(err)
		}, eventualTimeout, pollInterval).Should(BeTrue())
	})

	It("creates a PDB when configured", func() {
		minAvailable := intstr.FromInt(1)
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "pdb-app", Namespace: namespace},
			Spec: forgev1alpha1.ApplicationSpec{
				Image: "nginx:latest",
				PDB:   &forgev1alpha1.PDBSpec{MinAvailable: &minAvailable},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, app)
		})

		pdb := &policyv1.PodDisruptionBudget{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "pdb-app-pdb", Namespace: namespace}, pdb)
		}, eventualTimeout, pollInterval).Should(Succeed())
		Expect(pdb.Spec.MinAvailable.String()).To(Equal("1"))
	})

	It("creates an Ingress when configured", func() {
		pathType := networkingv1.PathTypePrefix
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "ingress-app", Namespace: namespace},
			Spec: forgev1alpha1.ApplicationSpec{
				Image: "nginx:latest",
				Ingress: &forgev1alpha1.IngressSpec{
					Host:     "example.com",
					Path:     "/",
					PathType: &pathType,
				},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, app)
		})

		ing := &networkingv1.Ingress{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "ingress-app", Namespace: namespace}, ing)
		}, eventualTimeout, pollInterval).Should(Succeed())
		Expect(ing.Spec.Rules[0].Host).To(Equal("example.com"))
	})

	It("creates a ConfigMap even with empty data, and cleans up the old name after a rename", func() {
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "cm-app", Namespace: namespace},
			Spec: forgev1alpha1.ApplicationSpec{
				Image:     "nginx:latest",
				ConfigMap: &forgev1alpha1.ConfigSpec{Name: "cm-app-old"},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, app)
		})

		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "cm-app-old", Namespace: namespace}, &corev1.ConfigMap{})
		}, eventualTimeout, pollInterval).Should(Succeed())

		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cm-app", Namespace: namespace}, app); err != nil {
				return err
			}
			app.Spec.ConfigMap.Name = "cm-app-new"
			return k8sClient.Update(ctx, app)
		}, eventualTimeout, pollInterval).Should(Succeed())

		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "cm-app-new", Namespace: namespace}, &corev1.ConfigMap{})
		}, eventualTimeout, pollInterval).Should(Succeed())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "cm-app-old", Namespace: namespace}, &corev1.ConfigMap{})
			return apierrors.IsNotFound(err)
		}, eventualTimeout, pollInterval).Should(BeTrue())
	})

	It("attaches a finalizer and fully deletes the Application when no storage is configured", func() {
		app := &forgev1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: "finalizer-app", Namespace: namespace},
			Spec:       forgev1alpha1.ApplicationSpec{Image: "nginx:latest"},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		Eventually(func() []string {
			got := &forgev1alpha1.Application{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "finalizer-app", Namespace: namespace}, got); err != nil {
				return nil
			}
			return got.Finalizers
		}, eventualTimeout, pollInterval).Should(ContainElement(ApplicationFinalizer))

		Expect(k8sClient.Delete(ctx, app)).To(Succeed())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "finalizer-app", Namespace: namespace}, &forgev1alpha1.Application{})
			return apierrors.IsNotFound(err)
		}, eventualTimeout, pollInterval).Should(BeTrue())
	})
})
