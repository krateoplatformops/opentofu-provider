// Package v1alpha1 contains API Schema definitions for the git v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=opentofu.krateo.io
// +versionName=v1alpha1
package v1alpha1

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// Package type metadata.
const (
	Group   = "opentofu.krateo.io"
	Version = "v1alpha1"
)

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: Group, Version: Version}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}
)

var (
	WorkspaceKind             = reflect.TypeOf(Workspace{}).Name()
	WorkspaceGroupKind        = schema.GroupKind{Group: Group, Kind: WorkspaceKind}.String()
	WorkspaceKindAPIVersion   = WorkspaceKind + "." + SchemeGroupVersion.String()
	WorkspaceGroupVersionKind = SchemeGroupVersion.WithKind(WorkspaceKind)
)

func init() {
	SchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}
