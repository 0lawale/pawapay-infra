package controller_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opsv1alpha1 "github.com/0lawale/configmirror-operator/api/v1alpha1"
	"github.com/0lawale/configmirror-operator/internal/controller"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = opsv1alpha1.AddToScheme(s)
	return s
}

func makeConfigMirror(name, ns, sourceNS string, targetNSes []string, matchLabels map[string]string) *opsv1alpha1.ConfigMirror {
	return &opsv1alpha1.ConfigMirror{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: opsv1alpha1.ConfigMirrorSpec{
			SourceNamespace:  sourceNS,
			TargetNamespaces: targetNSes,
			Selector:         metav1.LabelSelector{MatchLabels: matchLabels},
		},
	}
}

func makeConfigMap(name, ns string, lbls, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Data:       data,
	}
}

func makeNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestReconcile_MirrorsConfigMapToTargetNamespace(t *testing.T) {
	s := newScheme(t)
	ctx := context.Background()

	mirror := makeConfigMirror("test-mirror", "ops", "source-ns",
		[]string{"target-ns-1"}, map[string]string{"app": "myservice"})

	sourceCM := makeConfigMap("app-config", "source-ns",
		map[string]string{"app": "myservice"},
		map[string]string{"key": "value", "db_host": "postgres.internal"})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(mirror, sourceCM,
			makeNamespace("source-ns"), makeNamespace("target-ns-1"), makeNamespace("ops")).
		WithStatusSubresource(&opsv1alpha1.ConfigMirror{}).
		Build()

	r := &controller.ConfigMirrorReconciler{
		Client: fakeClient,
		Scheme: s,
		DB:     &controller.FakeDB{},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-mirror", Namespace: "ops"}}

	// First call: adds the finalizer and returns Requeue:true — no mirroring yet
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("first reconcile error: %v", err)
	}

	// Second call: finalizer already present, proceeds to full mirroring logic
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("second reconcile error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set after successful sync")
	}

	mirrored := &corev1.ConfigMap{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "app-config", Namespace: "target-ns-1"}, mirrored); err != nil {
		t.Fatalf("expected mirrored ConfigMap to exist in target-ns-1: %v", err)
	}
	if mirrored.Data["key"] != "value" {
		t.Errorf("expected key=value, got %q", mirrored.Data["key"])
	}
	if mirrored.Data["db_host"] != "postgres.internal" {
		t.Errorf("expected db_host=postgres.internal, got %q", mirrored.Data["db_host"])
	}
}

func TestReconcile_MirrorsToMultipleTargetNamespaces(t *testing.T) {
	s := newScheme(t)
	ctx := context.Background()

	mirror := makeConfigMirror("multi-mirror", "ops", "source-ns",
		[]string{"target-a", "target-b", "target-c"},
		map[string]string{"team": "platform"})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			mirror,
			makeConfigMap("platform-config", "source-ns",
				map[string]string{"team": "platform"}, map[string]string{"env": "dev"}),
			makeNamespace("source-ns"), makeNamespace("target-a"),
			makeNamespace("target-b"), makeNamespace("target-c"), makeNamespace("ops"),
		).
		WithStatusSubresource(&opsv1alpha1.ConfigMirror{}).
		Build()

	r := &controller.ConfigMirrorReconciler{
		Client: fakeClient, Scheme: s, DB: &controller.FakeDB{},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "multi-mirror", Namespace: "ops"}}

	// First call adds the finalizer
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	// Second call does the actual mirroring
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	for _, ns := range []string{"target-a", "target-b", "target-c"} {
		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: "platform-config", Namespace: ns}, cm); err != nil {
			t.Errorf("expected ConfigMap in namespace %s: %v", ns, err)
		}
	}
}

func TestReconcile_IgnoresConfigMapsNotMatchingSelector(t *testing.T) {
	s := newScheme(t)
	ctx := context.Background()

	mirror := makeConfigMirror("selective-mirror", "ops", "source-ns",
		[]string{"target-ns"}, map[string]string{"app": "myservice"})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			mirror,
			makeConfigMap("other-config", "source-ns",
				map[string]string{"app": "other-service"}, map[string]string{"k": "v"}),
			makeNamespace("source-ns"), makeNamespace("target-ns"), makeNamespace("ops"),
		).
		WithStatusSubresource(&opsv1alpha1.ConfigMirror{}).
		Build()

	r := &controller.ConfigMirrorReconciler{
		Client: fakeClient, Scheme: s, DB: &controller.FakeDB{},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "selective-mirror", Namespace: "ops"}}

	// Two calls: first adds finalizer, second runs the full reconcile
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "other-config", Namespace: "target-ns"}, cm); err == nil {
		t.Error("ConfigMap with wrong label should NOT have been mirrored")
	}
}

func TestReconcile_RequeuesWhenTargetNamespaceMissing(t *testing.T) {
	s := newScheme(t)
	ctx := context.Background()

	mirror := makeConfigMirror("missing-ns-mirror", "ops", "source-ns",
		[]string{"nonexistent-ns"}, map[string]string{"app": "svc"})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(mirror, makeNamespace("source-ns"), makeNamespace("ops")).
		WithStatusSubresource(&opsv1alpha1.ConfigMirror{}).
		Build()

	r := &controller.ConfigMirrorReconciler{
		Client: fakeClient, Scheme: s, DB: &controller.FakeDB{},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing-ns-mirror", Namespace: "ops"}}

	// First call adds the finalizer
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile error: %v", err)
	}

	// Second call hits the namespace validation and should requeue
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("expected no error (just requeue), got: %v", err)
	}
	if result.RequeueAfter < 1*time.Second {
		t.Errorf("expected RequeueAfter > 1s for missing namespace, got %v", result.RequeueAfter)
	}
}

func TestReconcile_AddsFinalizerOnFirstReconcile(t *testing.T) {
	s := newScheme(t)
	ctx := context.Background()

	mirror := makeConfigMirror("finalizer-test", "ops", "source-ns",
		[]string{"target-ns"}, map[string]string{"app": "svc"})

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(mirror, makeNamespace("source-ns"), makeNamespace("target-ns"), makeNamespace("ops")).
		WithStatusSubresource(&opsv1alpha1.ConfigMirror{}).
		Build()

	r := &controller.ConfigMirrorReconciler{
		Client: fakeClient, Scheme: s, DB: &controller.FakeDB{},
	}

	_, _ = r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "finalizer-test", Namespace: "ops"},
	})

	updated := &opsv1alpha1.ConfigMirror{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "finalizer-test", Namespace: "ops"}, updated)

	hasFinalizer := false
	for _, f := range updated.Finalizers {
		if f == "ops.pawapay.io/configmirror-finalizer" {
			hasFinalizer = true
		}
	}
	if !hasFinalizer {
		t.Error("expected finalizer to be added on first reconcile")
	}
}
