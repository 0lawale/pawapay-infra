package v1alpha1

// This file implements the runtime.Object interface for ConfigMirror and
// ConfigMirrorList by providing DeepCopyObject() and DeepCopy* helpers.
//
// Kubernetes requires every type registered with the scheme to implement
// runtime.Object, which needs DeepCopyObject(). This lets the API machinery
// safely clone objects without mutating the original — critical for the
// informer cache and reconcile loop.
//
// Normally kubebuilder's controller-gen generates this file automatically
// from the // +kubebuilder:object:root=true markers. Since we scaffolded
// manually we write it by hand — the logic is always the same pattern.

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// --- ConfigMirror ---

// DeepCopyObject implements runtime.Object.
func (in *ConfigMirror) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy returns a full deep copy of ConfigMirror.
func (in *ConfigMirror) DeepCopy() *ConfigMirror {
	if in == nil {
		return nil
	}
	out := new(ConfigMirror)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields of ConfigMirror into out.
func (in *ConfigMirror) DeepCopyInto(out *ConfigMirror) {
	*out = *in

	// TypeMeta and ObjectMeta are value types — already copied by *out = *in.
	// We only need to deep-copy fields that contain pointers, slices, or maps.
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)

	// Spec
	in.Spec.DeepCopyInto(&out.Spec)

	// Status
	in.Status.DeepCopyInto(&out.Status)
}

// --- ConfigMirrorList ---

// DeepCopyObject implements runtime.Object.
func (in *ConfigMirrorList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy returns a full deep copy of ConfigMirrorList.
func (in *ConfigMirrorList) DeepCopy() *ConfigMirrorList {
	if in == nil {
		return nil
	}
	out := new(ConfigMirrorList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields of ConfigMirrorList into out.
func (in *ConfigMirrorList) DeepCopyInto(out *ConfigMirrorList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)

	// Deep copy the slice of items
	if in.Items != nil {
		out.Items = make([]ConfigMirror, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// --- ConfigMirrorSpec ---

// DeepCopyInto copies all fields of ConfigMirrorSpec into out.
func (in *ConfigMirrorSpec) DeepCopyInto(out *ConfigMirrorSpec) {
	*out = *in

	// TargetNamespaces is a slice of strings — must be copied explicitly
	if in.TargetNamespaces != nil {
		out.TargetNamespaces = make([]string, len(in.TargetNamespaces))
		copy(out.TargetNamespaces, in.TargetNamespaces)
	}

	// Selector contains MatchLabels (map) and MatchExpressions (slice) — both need deep copy
	in.Selector.DeepCopyInto(&out.Selector)
}

// --- ConfigMirrorStatus ---

// DeepCopyInto copies all fields of ConfigMirrorStatus into out.
func (in *ConfigMirrorStatus) DeepCopyInto(out *ConfigMirrorStatus) {
	*out = *in

	// MirroredConfigMaps slice
	if in.MirroredConfigMaps != nil {
		out.MirroredConfigMaps = make([]MirroredConfigMap, len(in.MirroredConfigMaps))
		for i := range in.MirroredConfigMaps {
			in.MirroredConfigMaps[i].DeepCopyInto(&out.MirroredConfigMaps[i])
		}
	}

	// Conditions slice (metav1.Condition contains a string LastTransitionTime — value type, safe)
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
}

// --- MirroredConfigMap ---

// DeepCopyInto copies MirroredConfigMap fields into out.
func (in *MirroredConfigMap) DeepCopyInto(out *MirroredConfigMap) {
	*out = *in

	// LastSyncedAt is a *metav1.Time pointer — copy the value it points to
	if in.LastSyncedAt != nil {
		t := *in.LastSyncedAt
		out.LastSyncedAt = &t
	}
}
