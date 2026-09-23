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
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Ningendo7/forge-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "forge-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "forge-operator-controller-manager"

// controllerDeploymentName is the Deployment name after config/default's
// kustomize namePrefix ("forge-operator-") is applied to
// config/manager/manager.yaml's base name ("controller-manager").
const controllerDeploymentName = "forge-operator-controller-manager"

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
			By("validating that both controller-manager pods are running as expected")
			// config/manager/manager.yaml defaults to 2 replicas (see
			// finding #5's fix: the webhook server runs in this same pod,
			// so a single replica is a SPOF for every create/update to an
			// Application). This asserts that fix actually took effect,
			// not just that some copy is up.
			verifyControllerUp := func(g Gomega) {
				By("getting the names of the controller-manager pods")
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
				g.Expect(podNames).To(HaveLen(2), "expected 2 controller pods running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				By("validating both pods' status")
				for _, podName := range podNames {
					cmd = exec.Command("kubectl", "get",
						"pods", podName, "-o", "jsonpath={.status.phase}",
						"-n", namespace,
					)
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status for "+podName)
				}
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

		// Finding #9 (chaos scenario): proves the controller's 2-replica
		// leader-election setup actually delivers what it's for -- a
		// reconcile in flight surviving the leader pod disappearing, not
		// just serving admission webhooks from a spare. Doesn't try to hit
		// an exact "mid-reconcile" instant (unreliable to trigger from
		// outside a black-box deployed process); instead it kills the
		// current leader immediately after triggering a change that takes
		// real, multi-second work to converge (a Deployment scale-up -- new
		// pod scheduling/starting), which is a wide enough window to
		// reliably land the kill before convergence, and relies on
		// reconciliation being driven entirely by cluster state (not
		// in-memory state) for the new leader to finish the job from
		// scratch, not resume it.
		It("still converges a scaling change after the leader controller pod is killed mid-flight (crash-recovery regression)", func() {
			By("identifying the current leader controller pod via its leader-election Lease")
			cmd := exec.Command("kubectl", "get", "lease", "9429151e.ningendo7.github.io", "-n", namespace,
				"-o", "jsonpath={.spec.holderIdentity}")
			holderIdentity, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to read leader election Lease")
			Expect(holderIdentity).NotTo(BeEmpty())
			leaderPod := strings.SplitN(holderIdentity, "_", 2)[0]

			By("triggering a scaling change that takes real, multi-second work to converge")
			cmd = exec.Command("kubectl", "patch", "application", appName, "-n", appNamespace,
				"--type=merge", "-p", `{"spec":{"replicas":3}}`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch Application replicas")

			By("immediately killing the leader controller pod, before the scale-up has had time to converge")
			cmd = exec.Command("kubectl", "delete", "pod", leaderPod, "-n", namespace, "--wait=false")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete the leader controller pod")

			By("confirming the scale-up still converges to 3 available replicas despite the leader being killed mid-flight")
			deploymentName := appName + "-deployment"
			verifyScaledUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", deploymentName,
					"-n", appNamespace, "-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("3"))
			}
			Eventually(verifyScaledUp, 3*time.Minute, 2*time.Second).Should(Succeed())

			By("confirming the Application reports Ready again after recovering from the killed leader")
			verifyAppReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyAppReady, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("confirming both controller-manager pods are back to Running -- the killed one was recreated by the Deployment")
			verifyControllerPodsUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				podNames := utils.GetNonEmptyLines(output)
				g.Expect(podNames).To(HaveLen(2))
				for _, podName := range podNames {
					cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", "jsonpath={.status.phase}")
					phase, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(phase).To(Equal("Running"))
				}
				controllerPodName = podNames[0]
			}
			Eventually(verifyControllerPodsUp, 2*time.Minute, 2*time.Second).Should(Succeed())
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

		// Finding #9 (chaos scenario), regression-testing the reconcile-storm
		// fix: the primary watch on Application originally had no predicate,
		// so every status write (done unconditionally every reconcile)
		// re-triggered itself via that same watch -- an infinite,
		// self-sustaining loop (~4.6 reconciles/sec observed live). Fixed
		// with applicationChangePredicate (GenerationChanged OR
		// deletionTimestamp set). Proven here by scraping the real
		// controller_runtime_reconcile_total counter twice, a fixed interval
		// apart, once the Application has settled with no storage configured
		// (so there's no periodic resync requeue to account for either) -- a
		// storm would show dozens of reconciles in this window; a quiesced
		// controller shows ~0.
		It("does not keep reconciling a settled Application (reconcile-storm regression)", func() {
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred(), "Failed to get metrics reader token")

			By("confirming the Application is currently settled (Ready)")
			verifyAppReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", appName, "-n", appNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyAppReady, time.Minute, 2*time.Second).Should(Succeed())

			By("scraping the reconcile counter, waiting, then scraping it again")
			before, err := scrapeReconcileTotal(token)
			Expect(err).NotTo(HaveOccurred(), "Failed to scrape reconcile counter")

			const settleWindow = 20 * time.Second
			time.Sleep(settleWindow)

			after, err := scrapeReconcileTotal(token)
			Expect(err).NotTo(HaveOccurred(), "Failed to scrape reconcile counter")

			By("confirming the reconcile count barely moved -- a storm would show dozens of reconciles in this window")
			Expect(after-before).To(BeNumerically("<", 5),
				"expected at most a handful of reconciles for a settled Application over %s, got a delta of %v (before=%v, after=%v) -- possible reconcile-storm regression",
				settleWindow, after-before, before, after)
		})

		// Finding #9 (chaos scenario), regression-testing a real shipped fix:
		// Service never tracked .metadata.generation, so its Owns() watch was
		// wired to a generation-only predicate that silently never detected
		// direct edits (selector, ports, type) -- only deletion ever
		// triggered correction. Fixed with ownedContentChangedPredicate.
		// Proven here by patching the owned Service's selector directly (not
		// deleting it) and confirming the operator restores it on its own.
		It("corrects a direct edit to the owned Service, not just its deletion (drift regression)", func() {
			serviceName := appName

			By("reading the Service's original selector")
			cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", appNamespace,
				"-o", "jsonpath={.spec.selector.app}")
			originalSelector, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(originalSelector).NotTo(BeEmpty())

			By("directly patching the Service's selector to point at nothing real")
			cmd = exec.Command("kubectl", "patch", "service", serviceName, "-n", appNamespace,
				"--type=merge", "-p", `{"spec":{"selector":{"app":"e2e-drift-injected-wrong-value"}}}`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch Service selector")

			By("confirming the operator corrects the selector back on its own, without the Service ever being deleted")
			verifySelectorCorrected := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", appNamespace,
					"-o", "jsonpath={.spec.selector.app}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(originalSelector))
			}
			Eventually(verifySelectorCorrected, time.Minute, 2*time.Second).Should(Succeed())
		})

		// Finding #9 (chaos scenario), regression-testing a real shipped fix:
		// the validating webhook used to re-check live Secret existence on
		// every update, including a bare finalizer-removal update -- ordinary
		// namespace teardown deleting a Secret before a stuck-finalizer
		// Application could permanently deadlock its deletion with no way to
		// unstick it. Fixed two ways now (ValidateUpdate short-circuits
		// entirely once DeletionTimestamp is set; separately, a missing
		// Secret is only ever a Warning, never a rejection -- see finding #6
		// earlier in this same effort). This test targets the DeletionTimestamp
		// short-circuit specifically and deterministically: rather than racing
		// the real timing window the original bug depended on (Secret
		// disappearing between a successful cleanup and the finalizer-removal
		// write landing, which can't be reliably triggered from outside a
		// black-box deployed controller), it points storage at an unreachable
		// endpoint so cleanup never completes and the Application stays
		// Terminating on demand -- a stable window to prove an ordinary update
		// to it is never rejected, credentials Secret gone or not.
		It("never rejects an update to an Application that's already Terminating, even with its credentials Secret gone", func() {
			const deadlockAppName = "e2e-deadlock-app"
			const credsSecretName = "e2e-deadlock-creds"

			By("creating a fake AWS credentials Secret")
			cmd := exec.Command("kubectl", "create", "secret", "generic", credsSecretName, "-n", appNamespace,
				"--from-literal=AWS_ACCESS_KEY_ID=AKIAFAKEFAKEFAKEFAKE",
				"--from-literal=AWS_SECRET_ACCESS_KEY=fakefakefakefakefakefakefakefakefakefake")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create fake AWS credentials Secret")

			By("creating an Application pointed at an unreachable endpoint, so its cleanup can never complete")
			// deletionPolicy: Delete is explicit and load-bearing here --
			// Retain (the default) skips bucket cleanup and treats IAM/
			// access-key cleanup as best-effort/non-blocking (see
			// finalizeApplication's Retain branch), so the finalizer would
			// be removed almost immediately regardless of the unreachable
			// endpoint, defeating the whole point of this test.
			manifest := fmt.Sprintf(`
apiVersion: forge.ningendo7.github.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
spec:
  image: %s
  storage:
    provider: AWS
    bucket: e2e-deadlock-bucket
    secretName: %s
    endpoint: https://e2e-unreachable.invalid
    deletionPolicy: Delete
`, deadlockAppName, appNamespace, appImage, credsSecretName)
			applyManifest(manifest, "e2e-deadlock-app.yaml")

			By("deleting the credentials Secret the Application references")
			cmd = exec.Command("kubectl", "delete", "secret", credsSecretName, "-n", appNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete credentials Secret")

			By("deleting the Application -- it will stay Terminating since cleanup can never reach the unreachable endpoint")
			cmd = exec.Command("kubectl", "delete", "application", deadlockAppName, "-n", appNamespace, "--wait=false")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to issue delete for the Application")

			By("waiting for the Application to actually enter Terminating")
			verifyTerminating := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", deadlockAppName, "-n", appNamespace,
					"-o", "jsonpath={.metadata.deletionTimestamp}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
			}
			Eventually(verifyTerminating, time.Minute, 2*time.Second).Should(Succeed())

			By("confirming an ordinary update still succeeds while Terminating with its credentials Secret gone -- exactly what the old bug rejected")
			cmd = exec.Command("kubectl", "annotate", "application", deadlockAppName, "-n", appNamespace,
				"e2e-test/deadlock-probe=ok", "--overwrite")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "update to a Terminating Application was rejected -- the finalizer deadlock has regressed")

			By("force-clearing the finalizer so this Application doesn't linger forever chasing an unreachable cleanup target")
			cmd = exec.Command("kubectl", "patch", "application", deadlockAppName, "-n", appNamespace,
				"--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
			_, _ = utils.Run(cmd)
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

	// Finding #8 (IRSA round-trip proof) + finding #9 (cloud-bucket drift
	// self-healing): both genuinely need working, fast cloud calls
	// underneath, so they share one LocalStack deployment and one
	// AWS_ENDPOINT_URL-pointed controller-manager rather than each standing
	// one up. A sibling of "Application lifecycle", not nested in it, with
	// its own BeforeAll/AfterAll managing the LocalStack fixture and the env
	// override independently -- reverted in AfterAll so nothing after this
	// Context (if anything is ever added) has to care either way.
	Context("LocalStack-backed AWS scenarios", func() {
		const localstackNamespace = "forge-operator-e2e-localstack"
		const lsAppNamespace = "forge-operator-e2e-localstack-apps"
		const lsAppImage = "nginxinc/nginx-unprivileged:stable"
		// A fake but well-formed hostname/ARN -- LocalStack doesn't validate
		// that an OIDC provider or permissions-boundary policy actually
		// exists for these, it just needs strings to build the documents
		// with. Real values would only matter for an actual
		// AssumeRoleWithWebIdentity call, which is explicitly out of scope
		// (see the IRSA test's own comment below).
		const fakeOIDCHost = "localstack.example.com"
		const fakePermissionsBoundaryARN = "arn:aws:iam::000000000000:policy/e2e-irsa-boundary"
		localstackEndpoint := fmt.Sprintf("http://localstack.%s.svc.cluster.local:4566", localstackNamespace)

		BeforeAll(func() {
			By("creating namespaces for the LocalStack fixture and its Applications")
			cmd := exec.Command("kubectl", "create", "ns", localstackNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LocalStack namespace")
			cmd = exec.Command("kubectl", "create", "ns", lsAppNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LocalStack app namespace")

			By("deploying LocalStack (IAM/STS/S3 only)")
			cmd = exec.Command("kubectl", "apply", "-n", localstackNamespace, "-f", "testdata/localstack.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to deploy LocalStack")

			By("waiting for LocalStack to become available")
			cmd = exec.Command("kubectl", "wait", "--for=condition=Available", "deployment/localstack",
				"-n", localstackNamespace, "--timeout=3m")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "LocalStack did not become available")

			By("pointing the deployed controller-manager at LocalStack, with a short storage resync interval")
			cmd = exec.Command("kubectl", "set", "env", "deployment/"+controllerDeploymentName, "-n", namespace,
				"AWS_ENDPOINT_URL="+localstackEndpoint,
				"AWS_ACCESS_KEY_ID=test",
				"AWS_SECRET_ACCESS_KEY=test",
				"OIDC_PROVIDER_ARN=arn:aws:iam::000000000000:oidc-provider/"+fakeOIDCHost,
				"OIDC_PROVIDER_URL="+fakeOIDCHost,
				"APP_IRSA_PERMISSIONS_BOUNDARY_ARN="+fakePermissionsBoundaryARN,
				// Production default is 10m -- far too long for a test.
				// resolveStorageResyncInterval floors anything <=0, so this
				// only ever shortens it, never accidentally disables it.
				"STORAGE_RESYNC_INTERVAL=15s",
			)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to set LocalStack env on the controller-manager")

			By("waiting for the LocalStack env change to roll out")
			cmd = exec.Command("kubectl", "rollout", "status", "deployment/"+controllerDeploymentName,
				"-n", namespace, "--timeout=2m")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "controller-manager did not roll out with the LocalStack env")

			By("refreshing the controller-manager pod name for failure-log collection after the rollout")
			if podName, err := utils.Run(exec.Command("kubectl", "get", "pods",
				"-l", "control-plane=controller-manager", "-n", namespace,
				"-o", "jsonpath={.items[0].metadata.name}")); err == nil {
				controllerPodName = podName
			}
		})

		AfterAll(func() {
			By("reverting the controller-manager's env back to its real-AWS defaults")
			cmd := exec.Command("kubectl", "set", "env", "deployment/"+controllerDeploymentName, "-n", namespace,
				"AWS_ENDPOINT_URL-", "AWS_ACCESS_KEY_ID-", "AWS_SECRET_ACCESS_KEY-",
				"OIDC_PROVIDER_ARN-", "OIDC_PROVIDER_URL-", "APP_IRSA_PERMISSIONS_BOUNDARY_ARN-",
				"STORAGE_RESYNC_INTERVAL-")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "rollout", "status", "deployment/"+controllerDeploymentName,
				"-n", namespace, "--timeout=2m")
			_, _ = utils.Run(cmd)

			By("removing the LocalStack fixture and its app namespace")
			cmd = exec.Command("kubectl", "delete", "ns", localstackNamespace, "--ignore-not-found", "--wait=false")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "ns", lsAppNamespace, "--ignore-not-found", "--wait=false")
			_, _ = utils.Run(cmd)
		})

		// Finding #8: nothing in the unit or AWS-integration test suites
		// proves the IRSA trust policy this operator builds actually gets
		// persisted correctly -- the integration suite only checks the role
		// exists with the right shape, not the document's own content (see
		// s3/integration_test.go's doc comment). LocalStack stands in for
		// real AWS IAM/STS here so this test can drive a genuine
		// CreateRole/UpdateAssumeRolePolicy round-trip and read back what was
		// actually persisted, rather than comparing two values computed by
		// the same Go code. It still can't prove AWS's real OIDC condition
		// evaluator would honor the policy (LocalStack Community doesn't
		// enforce trust-policy conditions) -- only that this operator builds
		// and durably persists the right document for the real ServiceAccount
		// it created.
		It("provisions IRSA through LocalStack and the persisted trust policy matches the real ServiceAccount", func() {
			const irsaAppName = "e2e-irsa-localstack"

			By("creating a real S3-backed Application with no endpoint override, so it picks up the LocalStack env")
			manifest := fmt.Sprintf(`
apiVersion: forge.ningendo7.github.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
spec:
  image: %s
  storage:
    provider: AWS
    bucket: e2e-irsa-localstack-bucket
    region: us-east-1
`, irsaAppName, lsAppNamespace, lsAppImage)
			applyManifest(manifest, "e2e-irsa-localstack-app.yaml")
			defer func() {
				cmd := exec.Command("kubectl", "delete", "application", irsaAppName, "-n", lsAppNamespace,
					"--ignore-not-found", "--timeout=60s")
				_, _ = utils.Run(cmd)
			}()

			By("waiting for the Application to report Ready with a real IRSA role provisioned through LocalStack")
			var roleARN string
			verifyReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", irsaAppName, "-n", lsAppNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))

				cmd = exec.Command("kubectl", "get", "application", irsaAppName, "-n", lsAppNamespace,
					"-o", "jsonpath={.status.storage.aws.roleARN}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				roleARN = output
			}
			Eventually(verifyReady, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("reading back the trust policy LocalStack actually persisted, not just what the Go code intended to send")
			roleName := roleARN[strings.LastIndex(roleARN, "/")+1:]
			getRoleOutput, err := runAWSCLIInCluster(localstackEndpoint,
				[]string{"iam", "get-role", "--role-name=" + roleName, "--output=json"})
			Expect(err).NotTo(HaveOccurred(), "Failed to query LocalStack for the created IAM role")

			var getRoleResult struct {
				Role struct {
					Arn                      string `json:"Arn"`
					AssumeRolePolicyDocument struct {
						Statement []struct {
							Condition struct {
								StringEquals map[string]string `json:"StringEquals"`
							} `json:"Condition"`
						} `json:"Statement"`
					} `json:"AssumeRolePolicyDocument"`
				} `json:"Role"`
			}
			Expect(json.Unmarshal([]byte(getRoleOutput), &getRoleResult)).
				To(Succeed(), "LocalStack's get-role output was not valid JSON: %s", getRoleOutput)

			By("confirming the role LocalStack actually has matches what the Application's own status reports")
			Expect(getRoleResult.Role.Arn).To(Equal(roleARN))

			By("confirming the persisted trust policy's subject/audience match this Application's real ServiceAccount")
			Expect(getRoleResult.Role.AssumeRolePolicyDocument.Statement).NotTo(BeEmpty())
			condition := getRoleResult.Role.AssumeRolePolicyDocument.Statement[0].Condition.StringEquals
			expectedSubject := fmt.Sprintf("system:serviceaccount:%s:%s-sa", lsAppNamespace, irsaAppName)
			Expect(condition[fakeOIDCHost+":sub"]).To(Equal(expectedSubject))
			Expect(condition[fakeOIDCHost+":aud"]).To(Equal("sts.amazonaws.com"))
		})

		// Finding #9 (chaos scenario): "nothing else re-verifies a cloud
		// bucket still exists after creation" was a known gap this operator
		// closed with storageResyncInterval's periodic RequeueAfter -- but
		// nothing proved that requeue actually notices and self-heals real
		// drift, as opposed to just firing and no-op'ing. ensureBucketExists
		// (s3/desireds3.go) treats a HeadBucket 404 as "never existed,
		// create it" unconditionally, so the expected, correct behavior on
		// drift is silent recreation, not a Degraded condition -- this test
		// asserts exactly that: Ready never flips false, and the bucket
		// genuinely exists again afterward.
		It("recovers from cloud-bucket drift within the shortened resync interval", func() {
			const driftAppName = "e2e-drift-localstack"
			const bucket = "e2e-drift-localstack-bucket"

			By("creating a real S3-backed Application via LocalStack")
			manifest := fmt.Sprintf(`
apiVersion: forge.ningendo7.github.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
spec:
  image: %s
  storage:
    provider: AWS
    bucket: %s
    region: us-east-1
`, driftAppName, lsAppNamespace, lsAppImage, bucket)
			applyManifest(manifest, "e2e-drift-localstack-app.yaml")
			defer func() {
				cmd := exec.Command("kubectl", "delete", "application", driftAppName, "-n", lsAppNamespace,
					"--ignore-not-found", "--timeout=60s")
				_, _ = utils.Run(cmd)
			}()

			By("waiting for the Application to report Ready")
			verifyReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", driftAppName, "-n", lsAppNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyReady, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("deleting the bucket directly via LocalStack, out from under the operator")
			_, err := runAWSCLIInCluster(localstackEndpoint, []string{"s3", "rb", "s3://" + bucket, "--force"})
			Expect(err).NotTo(HaveOccurred(), "Failed to delete the bucket directly via LocalStack")

			By("confirming the bucket is genuinely gone before waiting on the resync")
			_, err = runAWSCLIInCluster(localstackEndpoint, []string{"s3api", "head-bucket", "--bucket=" + bucket})
			Expect(err).To(HaveOccurred(), "expected the bucket to be gone immediately after deleting it")

			By("confirming Ready never flips false -- the periodic resync should silently self-heal, not report drift as a failure")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "application", driftAppName, "-n", lsAppNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}, 45*time.Second, 3*time.Second).Should(Succeed())

			By("confirming the bucket actually exists again, recreated by the periodic resync")
			verifyRecreated := func(g Gomega) {
				_, err := runAWSCLIInCluster(localstackEndpoint, []string{"s3api", "head-bucket", "--bucket=" + bucket})
				g.Expect(err).NotTo(HaveOccurred(), "expected the periodic resync to have recreated the bucket by now")
			}
			Eventually(verifyRecreated, 30*time.Second, 3*time.Second).Should(Succeed())
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

// scrapeReconcileTotal creates a fresh ephemeral curl pod against the
// metrics endpoint (same auth/plumbing as the "should run successfully"
// test's curl-metrics pod above, but under its own pod name so it can be
// called repeatedly) and sums
// controller_runtime_reconcile_total{controller="application"} across all of
// its label combinations (result=success/error/requeue/...).
func scrapeReconcileTotal(token string) (float64, error) {
	const podName = "e2e-scrape-reconcile-total"
	_, _ = utils.Run(exec.Command("kubectl", "delete", "pod", podName, "-n", namespace, "--ignore-not-found"))
	defer func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "pod", podName, "-n", namespace, "--ignore-not-found"))
	}()

	cmd := exec.Command("kubectl", "run", podName, "--restart=Never",
		"--namespace", namespace,
		"--image=curlimages/curl:latest",
		"--overrides",
		fmt.Sprintf(`{
			"spec": {
				"containers": [{
					"name": "curl",
					"image": "curlimages/curl:latest",
					"command": ["/bin/sh", "-c"],
					"args": ["curl -s -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
					"securityContext": {
						"readOnlyRootFilesystem": true,
						"allowPrivilegeEscalation": false,
						"capabilities": {"drop": ["ALL"]},
						"runAsNonRoot": true,
						"runAsUser": 1000,
						"seccompProfile": {"type": "RuntimeDefault"}
					}
				}],
				"serviceAccountName": "%s"
			}
		}`, token, metricsServiceName, namespace, serviceAccountName))
	if _, err := utils.Run(cmd); err != nil {
		return 0, fmt.Errorf("failed to create %s pod: %w", podName, err)
	}

	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", "jsonpath={.status.phase}")
		output, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(output).To(Equal("Succeeded"))
	}, 2*time.Minute, 2*time.Second).Should(Succeed())

	output, err := utils.Run(exec.Command("kubectl", "logs", podName, "-n", namespace))
	if err != nil {
		return 0, err
	}

	var total float64
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "controller_runtime_reconcile_total{") || !strings.Contains(line, `controller="application"`) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		total += v
	}
	return total, nil
}

// runAWSCLIInCluster runs the AWS CLI with the given args from an ephemeral
// in-cluster pod against LocalStack's Service, then returns its stdout via
// kubectl logs -- the same pattern as the curl-metrics pod above. Run from
// inside the cluster (not port-forwarded to the test host) so the call
// exercises the same in-cluster network path the controller itself used, and
// to keep the AWS CLI image out of the host's own toolchain requirements. A
// non-zero AWS CLI exit code is returned as an error, with its stdout/stderr
// (captured together, same as `kubectl logs`) as the error text.
func runAWSCLIInCluster(endpoint string, args []string) (string, error) {
	const podName = "e2e-aws-cli"
	_, _ = utils.Run(exec.Command("kubectl", "delete", "pod", podName, "-n", namespace, "--ignore-not-found"))
	defer func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "pod", podName, "-n", namespace, "--ignore-not-found"))
	}()

	fullArgs := append([]string{"--endpoint-url=" + endpoint, "--region=us-east-1"}, args...)
	argsJSON, err := json.Marshal(fullArgs)
	if err != nil {
		return "", fmt.Errorf("failed to encode aws-cli args: %w", err)
	}

	cmd := exec.Command("kubectl", "run", podName, "--restart=Never",
		"--namespace", namespace,
		"--image=amazon/aws-cli:2.17.62",
		"--overrides",
		fmt.Sprintf(`{
			"spec": {
				"containers": [{
					"name": "aws-cli",
					"image": "amazon/aws-cli:2.17.62",
					"command": ["/usr/local/bin/aws"],
					"args": %s,
					"env": [
						{"name": "AWS_ACCESS_KEY_ID", "value": "test"},
						{"name": "AWS_SECRET_ACCESS_KEY", "value": "test"},
						{"name": "HOME", "value": "/tmp"}
					],
					"securityContext": {
						"allowPrivilegeEscalation": false,
						"capabilities": {"drop": ["ALL"]},
						"runAsNonRoot": true,
						"runAsUser": 1000,
						"seccompProfile": {"type": "RuntimeDefault"}
					}
				}]
			}
		}`, argsJSON))
	if _, err := utils.Run(cmd); err != nil {
		return "", fmt.Errorf("failed to create %s pod: %w", podName, err)
	}

	var phase string
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", "jsonpath={.status.phase}")
		output, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		phase = output
		g.Expect(phase).To(Or(Equal("Succeeded"), Equal("Failed")))
	}, 2*time.Minute, 2*time.Second).Should(Succeed())

	output, err := utils.Run(exec.Command("kubectl", "logs", podName, "-n", namespace))
	if err != nil {
		return "", err
	}
	if phase == "Failed" {
		return "", fmt.Errorf("aws-cli pod failed: %s", output)
	}
	return output, nil
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
