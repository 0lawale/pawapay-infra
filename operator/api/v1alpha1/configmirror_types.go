package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMirrorSpec defines the desired state.
type ConfigMirrorSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	SourceNamespace string `json:"sourceNamespace"`

	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Required
	TargetNamespaces []string `json:"targetNamespaces"`

	// +kubebuilder:validation:Required
	Selector metav1.LabelSelector `json:"selector"`
}

// MirroredConfigMap tracks one mirrored ConfigMap entry in status.
type MirroredConfigMap struct {
	Name            string       `json:"name"`
	SourceNamespace string       `json:"sourceNamespace"`
	TargetNamespace string       `json:"targetNamespace"`
	LastSyncedAt    *metav1.Time `json:"lastSyncedAt,omitempty"`
	SyncStatus      string       `json:"syncStatus"`
}

// ConfigMirrorStatus is the observed state written back by the operator.
type ConfigMirrorStatus struct {
	// +optional
	MirroredConfigMaps []MirroredConfigMap `json:"mirroredConfigMaps,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	TotalMirrored int `json:"totalMirrored,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cm-mirror
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceNamespace`
// +kubebuilder:printcolumn:name="Mirrored",type=integer,JSONPath=`.status.totalMirrored`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ConfigMirror struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigMirrorSpec   `json:"spec,omitempty"`
	Status ConfigMirrorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ConfigMirrorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigMirror `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigMirror{}, &ConfigMirrorList{})
}
