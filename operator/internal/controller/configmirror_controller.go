package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/0lawale/configmirror-operator/api/v1alpha1"
	"github.com/0lawale/configmirror-operator/internal/database"
)

const (
	finalizerName       = "ops.pawapay.io/configmirror-finalizer"
	requeueAfter        = 5 * time.Minute
	managedByLabel      = "ops.pawapay.io/managed-by"
	managedByLabelValue = "configmirror-operator"
	sourceMirrorLabel   = "ops.pawapay.io/source-mirror"
)

// DBClient is the interface the controller uses for persistence.
// Using an interface (not the concrete *database.Client directly) lets us
// swap in FakeDB during unit tests without touching real AWS/RDS.
type DBClient interface {
	UpsertMirroredConfigMap(ctx context.Context, record database.MirrorRecord) error
	DeleteMirroredConfigMap(ctx context.Context, mirrorName, mirrorNS, configmapName, targetNS string) error
	DeleteAllForMirror(ctx context.Context, mirrorName, mirrorNS string) error
}

// ConfigMirrorReconciler reconciles ConfigMirror objects.
//
// +kubebuilder:rbac:groups=ops.pawapay.io,resources=configmirrors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.pawapay.io,resources=configmirrors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.pawapay.io,resources=configmirrors/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
type ConfigMirrorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	DB     DBClient
}

func (r *ConfigMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("configmirror", req.NamespacedName)
	logger.Info("Reconcile started")

	// --- Fetch the ConfigMirror resource ---
	mirror := &opsv1alpha1.ConfigMirror{}
	if err := r.Get(ctx, req.NamespacedName, mirror); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching ConfigMirror: %w", err)
	}

	// --- Handle deletion ---
	if mirror.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, mirror)
	}

	// --- Ensure finalizer is registered ---
	if !controllerutil.ContainsFinalizer(mirror, finalizerName) {
		controllerutil.AddFinalizer(mirror, finalizerName)
		if err := r.Update(ctx, mirror); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// --- Validate target namespaces exist ---
	for _, ns := range mirror.Spec.TargetNamespaces {
		nsObj := &corev1.Namespace{}
		if err := r.Get(ctx, types.NamespacedName{Name: ns}, nsObj); err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("target namespace %q does not exist", ns)
				r.setCondition(mirror, "Ready", metav1.ConditionFalse, "NamespaceNotFound", msg)
				_ = r.Status().Update(ctx, mirror)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			return ctrl.Result{}, fmt.Errorf("checking namespace %s: %w", ns, err)
		}
	}

	// --- List matching ConfigMaps in source namespace ---
	selector, err := metav1.LabelSelectorAsSelector(&mirror.Spec.Selector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("parsing label selector: %w", err)
	}

	sourceConfigMaps := &corev1.ConfigMapList{}
	if err := r.List(ctx, sourceConfigMaps,
		client.InNamespace(mirror.Spec.SourceNamespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing source ConfigMaps: %w", err)
	}

	logger.Info("Found source ConfigMaps", "count", len(sourceConfigMaps.Items))

	// --- Mirror each ConfigMap to every target namespace ---
	var mirroredItems []opsv1alpha1.MirroredConfigMap
	var reconcileErrors []error

	for _, sourceCM := range sourceConfigMaps.Items {
		for _, targetNS := range mirror.Spec.TargetNamespaces {
			syncStatus, syncErr := r.mirrorConfigMap(ctx, mirror, sourceCM, targetNS)

			now := metav1.Now()
			mirroredItems = append(mirroredItems, opsv1alpha1.MirroredConfigMap{
				Name:            sourceCM.Name,
				SourceNamespace: mirror.Spec.SourceNamespace,
				TargetNamespace: targetNS,
				SyncStatus:      syncStatus,
				LastSyncedAt:    &now,
			})

			if syncErr != nil {
				reconcileErrors = append(reconcileErrors, syncErr)
				continue
			}

			dataJSON, _ := json.Marshal(sourceCM.Data)
			if dbErr := r.DB.UpsertMirroredConfigMap(ctx, database.MirrorRecord{
				ConfigMirrorName:      mirror.Name,
				ConfigMirrorNamespace: mirror.Namespace,
				ConfigMapName:         sourceCM.Name,
				SourceNamespace:       mirror.Spec.SourceNamespace,
				TargetNamespace:       targetNS,
				DataJSON:              dataJSON,
				SyncStatus:            syncStatus,
			}); dbErr != nil {
				logger.Error(dbErr, "Failed to persist mirror record to RDS")
			}
		}
	}

	// --- Garbage collect stale mirrored ConfigMaps ---
	if err := r.garbageCollect(ctx, mirror, sourceConfigMaps, selector); err != nil {
		reconcileErrors = append(reconcileErrors, err)
	}

	// --- Update status ---
	mirror.Status.MirroredConfigMaps = mirroredItems
	mirror.Status.TotalMirrored = len(mirroredItems)
	mirror.Status.ObservedGeneration = mirror.Generation

	if len(reconcileErrors) > 0 {
		r.setCondition(mirror, "Ready", metav1.ConditionFalse, "SyncErrors",
			fmt.Sprintf("%d error(s) during sync", len(reconcileErrors)))
	} else {
		r.setCondition(mirror, "Ready", metav1.ConditionTrue, "SyncedSuccessfully",
			fmt.Sprintf("Mirrored %d ConfigMap(s) to %d namespace(s)",
				len(sourceConfigMaps.Items), len(mirror.Spec.TargetNamespaces)))
	}

	if err := r.Status().Update(ctx, mirror); err != nil {
		logger.Error(err, "Failed to update status")
	}

	if len(reconcileErrors) > 0 {
		return ctrl.Result{}, reconcileErrors[0]
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *ConfigMirrorReconciler) mirrorConfigMap(
	ctx context.Context,
	mirror *opsv1alpha1.ConfigMirror,
	source corev1.ConfigMap,
	targetNS string,
) (string, error) {
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      source.Name,
			Namespace: targetNS,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		desired.Data = source.Data
		desired.BinaryData = source.BinaryData
		desired.Labels = mergeMaps(source.Labels, map[string]string{
			managedByLabel:    managedByLabelValue,
			sourceMirrorLabel: mirror.Name,
		})
		desired.Annotations = mergeMaps(source.Annotations, map[string]string{
			"ops.pawapay.io/source-namespace": source.Namespace,
			"ops.pawapay.io/source-configmap": source.Name,
		})
		return nil
	})

	if err != nil {
		return "Failed", fmt.Errorf("CreateOrUpdate ConfigMap %s/%s: %w", targetNS, source.Name, err)
	}

	return "Synced", nil
}

func (r *ConfigMirrorReconciler) garbageCollect(
	ctx context.Context,
	mirror *opsv1alpha1.ConfigMirror,
	currentSources *corev1.ConfigMapList,
	_ labels.Selector,
) error {
	sourceNames := make(map[string]bool, len(currentSources.Items))
	for _, cm := range currentSources.Items {
		sourceNames[cm.Name] = true
	}

	for _, targetNS := range mirror.Spec.TargetNamespaces {
		managed := &corev1.ConfigMapList{}
		if err := r.List(ctx, managed,
			client.InNamespace(targetNS),
			client.MatchingLabels{
				managedByLabel:    managedByLabelValue,
				sourceMirrorLabel: mirror.Name,
			},
		); err != nil {
			return fmt.Errorf("listing managed ConfigMaps in %s: %w", targetNS, err)
		}

		for _, cm := range managed.Items {
			if !sourceNames[cm.Name] {
				if err := r.Delete(ctx, &cm); err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("deleting stale ConfigMap %s/%s: %w", targetNS, cm.Name, err)
				}
				_ = r.DB.DeleteMirroredConfigMap(ctx, mirror.Name, mirror.Namespace, cm.Name, targetNS)
			}
		}
	}

	return nil
}

func (r *ConfigMirrorReconciler) handleDeletion(ctx context.Context, mirror *opsv1alpha1.ConfigMirror) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling ConfigMirror deletion")

	for _, targetNS := range mirror.Spec.TargetNamespaces {
		managed := &corev1.ConfigMapList{}
		if err := r.List(ctx, managed,
			client.InNamespace(targetNS),
			client.MatchingLabels{
				managedByLabel:    managedByLabelValue,
				sourceMirrorLabel: mirror.Name,
			},
		); err != nil {
			return ctrl.Result{}, fmt.Errorf("listing managed ConfigMaps for cleanup: %w", err)
		}

		for _, cm := range managed.Items {
			if err := r.Delete(ctx, &cm); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting ConfigMap %s/%s: %w", targetNS, cm.Name, err)
			}
		}
	}

	if err := r.DB.DeleteAllForMirror(ctx, mirror.Name, mirror.Namespace); err != nil {
		logger.Error(err, "Failed to delete DB records during cleanup")
	}

	controllerutil.RemoveFinalizer(mirror, finalizerName)
	if err := r.Update(ctx, mirror); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers this reconciler with controller-runtime.
// Uses handler.EnqueueRequestsFromMapFunc (the correct v0.17 API) to
// watch ConfigMaps and map them back to ConfigMirror reconcile requests.
func (r *ConfigMirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.ConfigMirror{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(ConfigMapToMirrors(mgr.GetClient())),
		).
		Complete(r)
}

// --- Helpers ---

func (r *ConfigMirrorReconciler) setCondition(
	mirror *opsv1alpha1.ConfigMirror,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	now := metav1.Now()
	for i, c := range mirror.Status.Conditions {
		if c.Type == condType {
			mirror.Status.Conditions[i] = metav1.Condition{
				Type:               condType,
				Status:             status,
				Reason:             reason,
				Message:            message,
				LastTransitionTime: now,
				ObservedGeneration: mirror.Generation,
			}
			return
		}
	}
	mirror.Status.Conditions = append(mirror.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		ObservedGeneration: mirror.Generation,
	})
}

func mergeMaps(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}
