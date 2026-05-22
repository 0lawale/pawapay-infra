package main

import (
	"context"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	opsv1alpha1 "github.com/0lawale/configmirror-operator/api/v1alpha1"
	"github.com/0lawale/configmirror-operator/internal/controller"
	"github.com/0lawale/configmirror-operator/internal/database"

	gozap "go.uber.org/zap"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(opsv1alpha1.AddToScheme(scheme))
}

func main() {
	// --- Logger setup ---
	// controller-runtime v0.17 uses zap.Options / zap.New from the ctrl/log/zap package
	opts := zap.Options{Development: false}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	// --- SSM paths from environment variables ---
	ssmPaths := database.SSMPaths{
		Username: requireEnv("SSM_DB_USERNAME_PATH"),
		Password: requireEnv("SSM_DB_PASSWORD_PATH"),
		Endpoint: requireEnv("SSM_DB_ENDPOINT_PATH"),
		DBName:   getEnvOrDefault("DB_NAME", "configmirror"),
	}

	// --- Connect to RDS via SSM / IRSA ---
	setupLog.Info("Connecting to RDS via SSM Parameter Store")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build a plain go.uber.org/zap logger for the database package
	// (separate from the controller-runtime logr logger)
	rawLogger, err := gozap.NewProduction()
	if err != nil {
		setupLog.Error(err, "Failed to build zap logger")
		os.Exit(1)
	}
	defer rawLogger.Sync() //nolint:errcheck

	dbClient, err := database.NewClient(ctx, ssmPaths, rawLogger)
	if err != nil {
		setupLog.Error(err, "Failed to connect to RDS")
		os.Exit(1)
	}
	defer func() {
		if err := dbClient.Close(); err != nil {
			setupLog.Error(err, "Error closing DB connection")
		}
	}()

	// --- Create controller-runtime manager ---
	// NOTE: In controller-runtime v0.17 the Metrics and HealthProbe addresses
	// moved out of Options{} and into sub-structs. MetricsBindAddress is now
	// set via Metrics.BindAddress, not a top-level string field.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: getEnvOrDefault("METRICS_ADDR", ":8080"),
		},
		HealthProbeBindAddress: getEnvOrDefault("HEALTH_PROBE_ADDR", ":8081"),
		LeaderElection:          true,
		LeaderElectionID:        "configmirror-operator-leader",
		LeaderElectionNamespace: getEnvOrDefault("OPERATOR_NAMESPACE", "ops"),
	})
	if err != nil {
		setupLog.Error(err, "Unable to create manager")
		os.Exit(1)
	}

	// --- Register the reconciler ---
	if err := (&controller.ConfigMirrorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		DB:     dbClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to set up ConfigMirror controller")
		os.Exit(1)
	}

	// --- Health and readiness probes ---
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up readiness check")
		os.Exit(1)
	}

	// --- Start (blocks until SIGTERM) ---
	setupLog.Info("Starting ConfigMirror operator", "version", "v0.1.0")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Manager exited with error")
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		ctrl.Log.Error(nil, "Required environment variable not set", "variable", key)
		os.Exit(1)
	}
	return v
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
