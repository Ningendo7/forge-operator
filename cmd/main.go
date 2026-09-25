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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"os"
	"strconv"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	"github.com/Ningendo7/forge-operator/internal/controller/ratelimit"
	statusmanager "github.com/Ningendo7/forge-operator/internal/controller/status"
	webhookv1alpha1 "github.com/Ningendo7/forge-operator/internal/webhook/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(forgev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// envFloat reads name as a float64, returning fallback if unset or
// unparseable -- a typo'd or missing rate-limit env var should degrade to
// a conservative default, not crash the manager at startup.
func envFloat(name string, fallback float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

// envDuration reads name via time.ParseDuration (e.g. "15s", "10m"),
// returning fallback if unset or unparseable -- same degrade-don't-crash
// reasoning as envInt/envFloat.
func envDuration(name string, fallback time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	// Development:false gives structured JSON logs by default, which is what
	// most cluster log pipelines expect; pass --zap-devel=true for the
	// human-readable console encoder when running locally.
	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Disabled by default: HTTP/2 Stream Cancellation and Rapid Reset CVEs
	// (GHSA-qppj-fm5r-hxr3, GHSA-4374-p667-p6c8).
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// Enforces authn/authz on the metrics endpoint; RBAC lives in
		// config/rbac/kustomization.yaml.
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// With no certificate specified, controller-runtime self-signs one --
	// fine for dev, not for production. To use cert-manager instead, enable
	// [METRICS-WITH-CERTS] in config/default/kustomization.yaml.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	// secretCacheSelector restricts the manager's informer cache for Secrets to
	// only the ones this operator itself creates and labels (see
	// controller.SecretRoleLabel). Without it, Owns(&corev1.Secret{}) in
	// ApplicationReconciler.SetupWithManager would cache every Secret in
	// every namespace cluster-wide in this pod's memory. Arbitrary
	// user-supplied credentials Secrets (spec.storage.secretName,
	// spec.storage.akamai.accessKeySecretRef) are exempted from the cache
	// entirely below (Client.Cache.DisableFor) so reads of those still work.
	secretRoleExists, err := labels.NewRequirement(controller.SecretRoleLabel, selection.Exists, nil)
	if err != nil {
		setupLog.Error(err, "Failed to build Secret cache label selector")
		os.Exit(1)
	}
	secretCacheSelector := labels.NewSelector().Add(*secretRoleExists)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9429151e.ningendo7.github.io",
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Secret{}: {Label: secretCacheSelector},
			},
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				// Belt-and-suspenders alongside the selector above: any
				// Secret read/list this operator issues goes straight to
				// the API server, live, rather than through the
				// (already label-scoped) cache -- covers reads of Secrets
				// this operator doesn't own or label at all, like
				// spec.storage.secretName.
				DisableFor: []client.Object{&corev1.Secret{}},
			},
		},
		// LeaderElectionReleaseOnCancel speeds up leader transitions but requires
		// the binary to exit immediately once the Manager stops -- unsafe if
		// anything here ever runs cleanup after that point.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	// Tracing is opt-in: only initialized if OTEL_EXPORTER_OTLP_ENDPOINT is
	// actually set, so a deployment with nothing running to receive traces
	// (e.g. no Jaeger) makes zero network attempts and pays zero cost --
	// every span created via forgemetrics.Tracer().Start elsewhere in this
	// operator is a documented no-op until this runs.
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		shutdownTracing, err := forgemetrics.Init(ctx)
		if err != nil {
			setupLog.Error(err, "Failed to initialize OpenTelemetry tracing")
			os.Exit(1)
		}
		defer func() {
			// A fresh, short-lived context, deliberately not ctx -- ctx is
			// already cancelled by the time this runs (mgr.Start returned
			// because the signal handler fired), but the shutdown flush
			// still needs its own brief window to actually send whatever
			// spans were buffered.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownTracing(shutdownCtx); err != nil {
				setupLog.Error(err, "Failed to shut down OpenTelemetry tracing cleanly")
			}
		}()
		setupLog.Info("OpenTelemetry tracing enabled", "endpoint", endpoint)
	} else {
		setupLog.Info("OpenTelemetry tracing disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)")
	}

	oidcProviderARN := os.Getenv("OIDC_PROVIDER_ARN")
	oidcProviderURL := os.Getenv("OIDC_PROVIDER_URL")
	defaultAkamaiRegion := os.Getenv("DEFAULT_AKAMAI_REGION")
	permissionsBoundaryARN := os.Getenv("APP_IRSA_PERMISSIONS_BOUNDARY_ARN")

	// Rate limiting for this operator's own outgoing AWS/Akamai calls --
	// independent of MaxConcurrentReconciles. Each surface gets its own
	// limiter (see internal/controller/ratelimit's package doc for why).
	// Defaults here are deliberately conservative starting points, not
	// verified figures for any specific AWS/Linode account tier -- tune
	// via these env vars once you know your account's real limits.
	s3RateLimiter := ratelimit.NewLimiter(
		envFloat("AWS_S3_RATE_LIMIT_QPS", 20),
		envInt("AWS_S3_RATE_LIMIT_BURST", 40),
	)
	iamRateLimiter := ratelimit.NewLimiter(
		envFloat("AWS_IAM_RATE_LIMIT_QPS", 8),
		envInt("AWS_IAM_RATE_LIMIT_BURST", 16),
	)
	akamaiAccountRateLimiter := ratelimit.NewLimiter(
		envFloat("AKAMAI_ACCOUNT_RATE_LIMIT_QPS", 5),
		envInt("AKAMAI_ACCOUNT_RATE_LIMIT_BURST", 10),
	)
	akamaiObjectRateLimiter := ratelimit.NewLimiter(
		envFloat("AKAMAI_OBJECT_RATE_LIMIT_QPS", 20),
		envInt("AKAMAI_OBJECT_RATE_LIMIT_BURST", 40),
	)

	// How many Applications this controller reconciles in parallel --
	// independent of the rate limiters above, which bound external call
	// *rate*, not reconcile *parallelism*. 5 matches this controller's
	// original, only-ever value from before this was configurable.
	maxConcurrentReconciles := envInt("MAX_CONCURRENT_RECONCILES", 5)

	// How often a settled, storage-backed Application re-verifies its cloud
	// bucket still exists. Configurable down from the 10-minute production
	// default so e2e drift-detection tests can prove this actually works
	// within a bounded wait rather than trusting the code path exists.
	storageResyncInterval := envDuration("STORAGE_RESYNC_INTERVAL", 10*time.Minute)

	if err := (&controller.ApplicationReconciler{
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		Recorder:                 mgr.GetEventRecorder("forge-operator"),
		OIDCProviderARN:          oidcProviderARN,
		OIDCProviderURL:          oidcProviderURL,
		PermissionsBoundaryARN:   permissionsBoundaryARN,
		DefaultAkamaiRegion:      defaultAkamaiRegion,
		S3RateLimiter:            s3RateLimiter,
		IAMRateLimiter:           iamRateLimiter,
		AkamaiAccountRateLimiter: akamaiAccountRateLimiter,
		AkamaiObjectRateLimiter:  akamaiObjectRateLimiter,
		MaxConcurrentReconciles:  maxConcurrentReconciles,
		StorageResyncInterval:    storageResyncInterval,
		StatusManager:            statusmanager.NewStatusManager(mgr.GetClient(), mgr.GetAPIReader()),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "application")
		os.Exit(1)
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupApplicationWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "Application")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	const rateLimitRampDuration = 30 * time.Second

	go func() {
		select {
		case <-mgr.Elected():
		case <-ctx.Done():
			return
		}
		for _, limiter := range []*rate.Limiter{
			s3RateLimiter,
			iamRateLimiter,
			akamaiAccountRateLimiter,
			akamaiObjectRateLimiter,
		} {
			go ratelimit.RampUp(ctx, limiter, rateLimitRampDuration)
		}
	}()
	setupLog.Info("Starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}
