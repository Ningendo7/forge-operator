//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Ningendo7/forge-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "forge-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "forge-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "forge-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "forge-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				By("getting the name of the controller-manager pod")
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				By("validating the pod's status")
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=forge-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			By("waiting for the webhook service endpoints to be ready")
			verifyWebhookEndpointsReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpointslices.discovery.k8s.io", "-n", namespace,
					"-l", "kubernetes.io/service-name=forge-operator-webhook-service",
					"-o", "jsonpath={range .items[*]}{range .endpoints[*]}{.addresses[*]}{end}{end}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Webhook endpoints should exist")
				g.Expect(output).ShouldNot(BeEmpty(), "Webhook endpoints not yet ready")
			}
			Eventually(verifyWebhookEndpointsReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying the mutating webhook server is ready")
			verifyMutatingWebhookReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "mutatingwebhookconfigurations.admissionregistration.k8s.io",
					"forge-operator-mutating-webhook-configuration",
					"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "MutatingWebhookConfiguration should exist")
				g.Expect(output).ShouldNot(BeEmpty(), "Mutating webhook CA bundle not yet injected")
			}
			Eventually(verifyMutatingWebhookReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying the validating webhook server is ready")
			verifyValidatingWebhookReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "validatingwebhookconfigurations.admissionregistration.k8s.io",
					"forge-operator-validating-webhook-configuration",
					"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "ValidatingWebhookConfiguration should exist")
				g.Expect(output).ShouldNot(BeEmpty(), "Validating webhook CA bundle not yet injected")
			}
			Eventually(verifyValidatingWebhookReady, 3*time.Minute, time.Second).Should(Succeed())

			By("waiting additional time for webhook server to stabilize")
			time.Sleep(5 * time.Second)

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [
								"for i in $(seq 1 30); do curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
							],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		It("should provisioned cert-manager", func() {
			By("validating that cert-manager has the certificate Secret")
			verifyCertManager := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "secrets", "webhook-server-cert", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyCertManager).Should(Succeed())
		})

		It("should have CA injection for mutating webhooks", func() {
			By("checking CA injection for mutating webhooks")
			verifyCAInjection := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"mutatingwebhookconfigurations.admissionregistration.k8s.io",
					"forge-operator-mutating-webhook-configuration",
					"-o", "go-template={{ range .webhooks }}{{ .clientConfig.caBundle }}{{ end }}")
				mwhOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(mwhOutput)).To(BeNumerically(">", 10))
			}
			Eventually(verifyCAInjection).Should(Succeed())
		})

		It("should have CA injection for validating webhooks", func() {
			By("checking CA injection for validating webhooks")
			verifyCAInjection := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"validatingwebhookconfigurations.admissionregistration.k8s.io",
					"forge-operator-validating-webhook-configuration",
					"-o", "go-template={{ range .webhooks }}{{ .clientConfig.caBundle }}{{ end }}")
				vwhOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(vwhOutput)).To(BeNumerically(">", 10))
			}
			Eventually(verifyCAInjection).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	// Application lifecycle exercises real-cluster behavior that envtest cannot:
	// the controller-manager's actually-deployed RBAC (envtest's client is an
	// admin client and would never notice a missing verb in config/rbac/role.yaml),
	// a real kubelet actually running the pod, the real disruption controller
	// enforcing a PodDisruptionBudget (envtest doesn't run kube-controller-manager,
	// so PDB.status.disruptionsAllowed is never computed there), and real garbage
	// collection of owned resources on delete. It runs after the "Manager" context
	// above so the controller-manager and CRDs are already installed and running.
	Context("Application lifecycle", func() {
		const appNamespace = "forge-operator-e2e-apps"
		const appName = "e2e-lifecycle-app"
		// Runs as non-root and listens on 8080 by default, matching both the
		// operator's restricted-by-default PodSecurityContext and the
		// ContainerSpec.Port default, so no overrides are needed to reach Ready.
		const appImage = "nginxinc/nginx-unprivileged:stable"

		var podName string

		BeforeAll(func() {
			By("creating a namespace for the Application lifecycle tests")
			cmd := exec.Command("kubectl", "create", "ns", appNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create app namespace")
		})

		AfterAll(func() {
			By("removing the Application lifecycle test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", appNamespace, "--ignore-not-found", "--wait=false")
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			specReport := CurrentSpecReport()
			if specReport.Failed() {
				By("Fetching Application status for debugging")
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace, "-o", "yaml")
				if output, err := utils.Run(cmd); err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Application:\n%s", output)
				}

				By("Fetching pod events for debugging")
				cmd = exec.Command("kubectl", "get", "events", "-n", appNamespace, "--sort-by=.lastTimestamp")
				if output, err := utils.Run(cmd); err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Events:\n%s", output)
				}
			}
		})

		It("reconciles a real Application into a running, ready pod under the deployed RBAC", func() {
			By("applying a minimal Application with a PDB")
			manifest := fmt.Sprintf(`
apiVersion: forge.ningendo7.github.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
spec:
  image: %s
  replicas: 1
  pdb:
    minAvailable: 1
`, appName, appNamespace, appImage)
			applyManifest(manifest, "e2e-application-lifecycle.yaml")

			By("waiting for the Deployment to report an available replica")
			deploymentName := appName + "-deployment"
			verifyDeploymentAvailable := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", deploymentName,
					"-n", appNamespace, "-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}
			Eventually(verifyDeploymentAvailable, 3*time.Minute, 2*time.Second).Should(Succeed())

			By("finding the actually-running pod behind the Deployment")
			verifyPodRunning := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", appNamespace,
					"-l", fmt.Sprintf("app=%s", appName),
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				podName = output

				cmd = exec.Command("kubectl", "get", "pod", podName, "-n", appNamespace,
					"-o", "jsonpath={.status.phase}")
				phase, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(phase).To(Equal("Running"))
			}
			Eventually(verifyPodRunning, 3*time.Minute, 2*time.Second).Should(Succeed())

			By("confirming the Service was created")
			cmd := exec.Command("kubectl", "get", "service", appName, "-n", appNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Service should exist")

			By("confirming the Application reports Ready via the real deployed controller")
			verifyAppReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyAppReady, 2*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("lets the real disruption controller block eviction once the PDB has no spare budget", func() {
			pdbName := appName + "-pdb"

			By("waiting for the disruption controller to compute a zero disruption budget")
			verifyNoDisruptionsAllowed := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pdb", pdbName, "-n", appNamespace,
					"-o", "jsonpath={.status.disruptionsAllowed}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("0"))
			}
			Eventually(verifyNoDisruptionsAllowed, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("attempting to evict the pod via the eviction subresource")
			evictionRequest := fmt.Sprintf(`{
	"apiVersion": "policy/v1",
	"kind": "Eviction",
	"metadata": {"name": %q, "namespace": %q}
}`, podName, appNamespace)
			evictionFile := filepath.Join("/tmp", "e2e-eviction-request.json")
			Expect(os.WriteFile(evictionFile, []byte(evictionRequest), 0o644)).To(Succeed())

			cmd := exec.Command("kubectl", "create", "--raw",
				fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/eviction", appNamespace, podName),
				"-f", evictionFile)
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "Eviction should have been rejected by the disruption budget")
			Expect(string(output)).To(ContainSubstring("Cannot evict pod as it would violate the pod's disruption budget."))

			By("confirming the pod was not actually evicted")
			cmd = exec.Command("kubectl", "get", "pod", podName, "-n", appNamespace,
				"-o", "jsonpath={.status.phase}")
			phase, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal("Running"))
		})

		It("converges the real Deployment when the Application is scaled up", func() {
			By("patching the Application to increase replicas")
			cmd := exec.Command("kubectl", "patch", "application", appName, "-n", appNamespace,
				"--type=merge", "-p", `{"spec":{"replicas":2}}`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch Application replicas")

			By("waiting for the Deployment to converge to 2 available replicas")
			deploymentName := appName + "-deployment"
			verifyScaledUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", deploymentName,
					"-n", appNamespace, "-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("2"))
			}
			Eventually(verifyScaledUp, 3*time.Minute, 2*time.Second).Should(Succeed())

			By("confirming the Application reports Ready again after the update")
			verifyAppReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyAppReady, 2*time.Minute, 2*time.Second).Should(Succeed())
		})

		// This is the scenario envtest explicitly can't cover (see the comment
		// atop application_integration_test.go: AWS/Akamai storage paths are
		// out of scope there). It doesn't need real AWS credentials or
		// network access, though: the credentials Secret exists and has the
		// required keys, so it clears admission, but spec.storage.endpoint
		// points at a host that can never resolve, so reconcileAWSStorage's
		// first real AWS API call fails fast and locally with a DNS error --
		// it never actually reaches AWS. That's enough to prove, against the
		// actually-deployed controller, that a real storage misconfiguration
		// surfaces as Degraded (not a crash-loop or a silent stall) and that
		// fixing it lets the Application recover on its own.
		//
		// Deliberately AWS, not Akamai: the admission webhook now checks both
		// providers' credentials Secret exists with the required keys (see
		// the "rejects an invalid Akamai storage config at admission time"
		// test below), so a merely-missing Secret no longer reaches the
		// reconciler for either provider. Endpoint/region validity, though,
		// is deliberately left unchecked at admission for both providers
		// (see docs/development-and-operations.md) -- this test exercises
		// that remaining gap via AWS, which is exercised elsewhere in this
		// file too, so a failure here is easier to place.
		It("reports Degraded on a real storage misconfiguration, then recovers to Ready once fixed", func() {
			const credsSecretName = "e2e-fake-aws-creds"

			By("creating a credentials Secret that satisfies admission but isn't a real AWS key pair")
			cmd := exec.Command("kubectl", "create", "secret", "generic", credsSecretName,
				"-n", appNamespace,
				"--from-literal=AWS_ACCESS_KEY_ID=AKIAFAKEFAKEFAKEFAKE",
				"--from-literal=AWS_SECRET_ACCESS_KEY=fakefakefakefakefakefakefakefakefakefake")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create fake AWS credentials Secret")

			By("patching in a storage config pointed at an endpoint that can never be reached")
			cmd = exec.Command("kubectl", "patch", "application", appName, "-n", appNamespace,
				"--type=merge", "-p",
				fmt.Sprintf(`{"spec":{"storage":{"provider":"AWS","bucket":"e2e-test-bucket",`+
					`"secretName":%q,"endpoint":"https://e2e-unreachable.invalid"}}}`, credsSecretName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch Application storage")

			By("waiting for the Application to report Degraded")
			verifyDegraded := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Degraded')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyDegraded, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("confirming the controller-manager kept running through the failure, rather than crash-looping")
			cmd = exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace, "-o", "jsonpath={.status.phase}")
			phase, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal("Running"))

			By("removing the broken storage config")
			cmd = exec.Command("kubectl", "patch", "application", appName, "-n", appNamespace,
				"--type=merge", "-p", `{"spec":{"storage":null}}`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to remove Application storage")

			By("cleaning up the fake credentials Secret")
			cmd = exec.Command("kubectl", "delete", "secret", credsSecretName, "-n", appNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("confirming the Application recovers to Ready on its own")
			verifyAppReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyAppReady, 2*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("rejects an invalid Akamai storage config at admission time via the validating webhook", func() {
			By("attempting to create an Application whose Akamai secretName collides with its accessKeySecretRef")
			manifest := fmt.Sprintf(`
apiVersion: forge.ningendo7.github.io/v1alpha1
kind: Application
metadata:
  name: e2e-webhook-rejected-app
  namespace: %s
spec:
  image: %s
  storage:
    provider: Akamai
    bucket: e2e-test-bucket
    secretName: shared-secret
    akamai:
      accessKeySecretRef: shared-secret
`, appNamespace, appImage)
			manifestFile := filepath.Join("/tmp", "e2e-webhook-rejected-app.yaml")
			Expect(os.WriteFile(manifestFile, []byte(manifest), 0o644)).To(Succeed())

			// Retried for the same reason applyManifest is: cert-manager's CA
			// injection can lag briefly, which would otherwise show up as a
			// generic TLS/connection error instead of the webhook's own
			// rejection message.
			verifyRejected := func(g Gomega) {
				cmd := exec.Command("kubectl", "apply", "-f", manifestFile)
				output, err := cmd.CombinedOutput()
				g.Expect(err).To(HaveOccurred(), "Expected the admission webhook to reject this Application")
				g.Expect(string(output)).To(ContainSubstring("must not be the same Secret"))
			}
			Eventually(verifyRejected, time.Minute, 2*time.Second).Should(Succeed())

			By("confirming the rejected Application was never actually created")
			cmd := exec.Command("kubectl", "get", "application", "e2e-webhook-rejected-app", "-n", appNamespace)
			_, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "the Application should not exist since admission rejected it")
		})

		It("garbage collects owned resources via the real deployed controller when the Application is deleted", func() {
			By("deleting the Application")
			cmd := exec.Command("kubectl", "delete", "application", appName, "-n", appNamespace, "--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete Application")

			By("confirming the owned Deployment, Service, and PDB were garbage collected")
			deploymentName := appName + "-deployment"
			pdbName := appName + "-pdb"
			verifyResourcesGone := func(g Gomega) {
				for _, res := range []struct{ kind, name string }{
					{"deployment", deploymentName},
					{"service", appName},
					{"poddisruptionbudget", pdbName},
				} {
					cmd := exec.Command("kubectl", "get", res.kind, res.name, "-n", appNamespace)
					_, err := cmd.CombinedOutput()
					g.Expect(err).To(HaveOccurred(), fmt.Sprintf("%s/%s should have been garbage collected", res.kind, res.name))
				}
			}
			Eventually(verifyResourcesGone, 2*time.Minute, 2*time.Second).Should(Succeed())
		})
	})
})

// applyManifest writes the given YAML to a temp file named filename under /tmp
// and applies it with kubectl, so callers can inline manifests as Go string
// literals the same way serviceAccountToken inlines its JSON request body.
func applyManifest(yamlContent, filename string) {
	manifestFile := filepath.Join("/tmp", filename)
	ExpectWithOffset(1, os.WriteFile(manifestFile, []byte(yamlContent), 0o644)).To(Succeed())

	// Retried because cert-manager's CA injection into the webhook
	// configurations is asynchronous: right after `make deploy`, the
	// admission webhooks can be briefly unreachable (TLS handshake failure)
	// until the injected CA bundle lands, even though the controller pod
	// itself is already Running.
	EventuallyWithOffset(1, func() error {
		cmd := exec.Command("kubectl", "apply", "-f", manifestFile)
		_, err := utils.Run(cmd)
		return err
	}, time.Minute, 2*time.Second).Should(Succeed(), "Failed to apply manifest")
}

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	By("creating temporary file to store the token request")
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		By("executing kubectl command to create the token")
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		By("parsing the JSON output to extract the token")
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
