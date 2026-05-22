package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1alpha1 "github.com/0lawale/configmirror-operator/api/v1alpha1"
)

// ConfigMapToMirrors maps a ConfigMap change event to the ConfigMirror
// resources that care about it.
//
// controller-runtime v0.17 uses handler.MapFunc — a plain function instead
// of an interface. This is simpler: given any object, return the list of
// ConfigMirror reconcile requests that should fire.
func ConfigMapToMirrors(c client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cm, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return nil
		}

		// Skip ConfigMaps the operator itself manages — prevents infinite loops
		if cm.Labels[managedByLabel] == managedByLabelValue {
			return nil
		}

		// List all ConfigMirror resources across all namespaces
		mirrors := &opsv1alpha1.ConfigMirrorList{}
		if err := c.List(ctx, mirrors); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, mirror := range mirrors.Items {
			// Only trigger reconcile for mirrors watching this ConfigMap's namespace
			if mirror.Spec.SourceNamespace == cm.Namespace {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      mirror.Name,
						Namespace: mirror.Namespace,
					},
				})
			}
		}

		return requests
	}
}
