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
	TFConnectorKind             = reflect.TypeOf(TFConnector{}).Name()
	TFConnectorGroupKind        = schema.GroupKind{Group: Group, Kind: TFConnectorKind}.String()
	TFConnectorKindAPIVersion   = TFConnectorKind + "." + SchemeGroupVersion.String()
	TFConnectorGroupVersionKind = SchemeGroupVersion.WithKind(TFConnectorKind)
)

func init() {
	SchemeBuilder.Register(&TFConnector{}, &TFConnectorList{})
}
