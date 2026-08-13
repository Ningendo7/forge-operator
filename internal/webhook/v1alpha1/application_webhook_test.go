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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

var _ = Describe("Application Webhook", func() {
	const namespace = "default"
	const akamaiAppName = "akamai-app"
	const testBucket = "some-bucket"

	var (
		obj       *forgev1alpha1.Application
		oldObj    *forgev1alpha1.Application
		validator ApplicationCustomValidator
		defaulter ApplicationCustomDefaulter
	)

	BeforeEach(func() {
		obj = &forgev1alpha1.Application{}
		oldObj = &forgev1alpha1.Application{}
		validator = ApplicationCustomValidator{Client: k8sClient}
		defaulter = ApplicationCustomDefaulter{}
		Expect(oldObj).NotTo(BeNil())
		Expect(obj).NotTo(BeNil())
	})

	Context("When creating Application under Defaulting Webhook", func() {
		It("does nothing when storage is unset", func() {
			obj.Spec.Storage = nil
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Storage).To(BeNil())
		})

		It("leaves AWS's spec.storage.secretName untouched, since a non-empty value there also means \"use static credentials instead of IRSA\"", func() {
			obj.Name = "aws-app"
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAWSS3,
				Bucket:   testBucket,
			}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Storage.SecretName).To(BeEmpty(),
				"defaulting this would silently force the S3 manager into the static-credentials path")
		})

		It("defaults spec.storage.secretName for Akamai", func() {
			obj.Name = akamaiAppName
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:   testBucket,
			}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Storage.SecretName).To(Equal(akamaiAppName + "-storage"))
		})

		It("creates the Akamai block and defaults accessKeySecretRef when unset", func() {
			obj.Name = akamaiAppName
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:   testBucket,
			}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Storage.Akamai).NotTo(BeNil())
			Expect(obj.Spec.Storage.Akamai.AccessKeySecretRef).To(Equal(akamaiAppName + "-akamai-token"))
		})

		It("does not overwrite explicitly-set Akamai secret names", func() {
			obj.Name = akamaiAppName
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider:   forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:     testBucket,
				SecretName: "custom-output",
				Akamai:     &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "custom-token"},
			}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Storage.SecretName).To(Equal("custom-output"))
			Expect(obj.Spec.Storage.Akamai.AccessKeySecretRef).To(Equal("custom-token"))
		})
	})

	Context("When creating or updating Application under Validating Webhook", func() {
		It("admits an Application with no storage configured", func() {
			obj.Name = "no-storage-app"
			obj.Namespace = namespace
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("admits an AWS Application regardless of secretName, since collision rules don't apply there", func() {
			obj.Name = "aws-app"
			obj.Namespace = namespace
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider:   forgev1alpha1.ProviderAWSS3,
				Bucket:     testBucket,
				SecretName: "whatever",
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an Akamai Application where secretName collides with accessKeySecretRef", func() {
			obj.Name = "colliding-app"
			obj.Namespace = namespace
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider:   forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:     testBucket,
				SecretName: "shared-secret",
				Akamai:     &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "shared-secret"},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not be the same Secret"))
		})

		It("rejects an Akamai Application whose token Secret doesn't exist", func() {
			obj.Name = "missing-secret-app"
			obj.Namespace = namespace
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:   testBucket,
				Akamai:   &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "does-not-exist"},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("rejects an Akamai Application whose token Secret is missing the apiToken key", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "token-no-key", Namespace: namespace},
				Data:       map[string][]byte{"other-key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, secret) })

			obj.Name = "bad-token-app"
			obj.Namespace = namespace
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:   testBucket,
				Akamai:   &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "token-no-key"},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("apiToken"))
		})

		It("admits a valid Akamai Application with a distinct, existing token Secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "valid-token", Namespace: namespace},
				Data:       map[string][]byte{"apiToken": []byte("token123")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, secret) })

			obj.Name = "valid-akamai-app"
			obj.Namespace = namespace
			obj.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider:   forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:     testBucket,
				SecretName: "valid-akamai-app-storage",
				Akamai:     &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "valid-token"},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			By("validating updates the same way")
			_, err = validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
