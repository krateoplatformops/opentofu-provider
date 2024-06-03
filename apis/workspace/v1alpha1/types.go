package v1alpha1

import (
	commonv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Credentials required to authenticate.
type Credentials struct {
	// Filename (relative to main.tf) to which these provider credentials
	// should be written.
	Filename string `json:"filename"`

	// Source of the provider credentials.
	Credentials commonv1.CredentialSelectors `json:"credentials"`
}

// A Var represents a OpenTofu configuration variable.
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// A VarFileSource specifies the source of a OpenTofu vars file.
// +kubebuilder:validation:Enum=ConfigMapKey;SecretKey
type VarFileSource string

// Vars file sources.
const (
	VarFileSourceConfigMapKey VarFileSource = "ConfigMapKey"
	VarFileSourceSecretKey    VarFileSource = "SecretKey"
)

// A VarFileFormat specifies the format of a OpenTofu vars file.
// +kubebuilder:validation:Enum=HCL;JSON
type VarFileFormat string

// Vars file formats.
var (
	VarFileFormatHCL  VarFileFormat = "HCL"
	VarFileFormatJSON VarFileFormat = "JSON"
)

// A VarFile is a file containing many OpenTofu variables.
type VarFile struct {
	// Source of this vars file.
	Source VarFileSource `json:"source"`

	// Format of this vars file.
	// +kubebuilder:default=HCL
	// +optional
	Format *VarFileFormat `json:"format,omitempty"`

	// A ConfigMap key containing the vars file.
	// +optional
	ConfigMapKeyReference *KeyReference `json:"configMapKeyRef,omitempty"`

	// A Secret key containing the vars file.
	// +optional
	SecretKeyReference *KeyReference `json:"secretKeyRef,omitempty"`
}

// A KeyReference references a key within a Secret or a ConfigMap.
type KeyReference struct {
	// Namespace of the referenced resource.
	Namespace string `json:"namespace"`

	// Name of the referenced resource.
	Name string `json:"name"`

	// Key within the referenced resource.
	Key string `json:"key"`
}

// A ModuleSource represents the source of a OpenTofu module.
// +kubebuilder:validation:Enum=Remote;Inline
type ModuleSource string

// Module sources.
const (
	ModuleSourceRemote ModuleSource = "Remote"
	ModuleSourceInline ModuleSource = "Inline"
)

// WorkspaceParameters are the configurable fields of a Workspace.
type WorkspaceParameters struct {
	// The root module of this workspace; i.e. the module containing its main.tf
	// file. When the workspace's source is 'Remote' (the default) this can be
	// any address supported by tofu init -from-module, for example a git
	// repository or an S3 bucket. When the workspace's source is 'Inline' the
	// content of a simple main.tf file may be written inline.
	Module string `json:"module"`

	// // Source of the root module of this workspace.
	// Source ModuleSource `json:"source"`

	// // Entrypoint for `tofu init` within the module
	// // +kubebuilder:default=""
	// // +optional
	// Entrypoint string `json:"entrypoint"`

	// // Configuration variables.
	// // +optional
	// Vars []Var `json:"vars,omitempty"`

	// // Files of configuration variables. Explicitly declared vars take
	// // precedence.
	// // +optional
	// VarFiles []VarFile `json:"varFiles,omitempty"`

	// // Arguments to be included in the tofu init CLI command
	// InitArgs []string `json:"initArgs,omitempty"`

	// // Arguments to be included in the tofu plan CLI command
	// PlanArgs []string `json:"planArgs,omitempty"`

	// // Arguments to be included in the tofu apply CLI command
	// ApplyArgs []string `json:"applyArgs,omitempty"`

	// // Arguments to be included in the tofu destroy CLI command
	// DestroyArgs []string `json:"destroyArgs,omitempty"`

	// // Cloud - set this flag to true if running on terraform cloud
	// Cloud bool `json:"cloud,omitempty"`
}

// WorkspaceObservation are the observable fields of a Workspace.
// type WorkspaceObservation struct {
// 	Outputs map[string]string `json:"outputs,omitempty"`
// }

// A WorkspaceSpec defines the desired state of a Workspace.
type WorkspaceSpec struct {
	commonv1.ManagedSpec `json:",inline"`
	// ConnectorConfigRef: configuration spec for
	// +immutable
	TFConnectorRef *commonv1.Reference `json:"tfConnectorRef,omitempty"`
	// Workspace: configuration spec for the workspace.
	// +required
	Workspace WorkspaceParameters `json:"workspace"`
}

// A WorkspaceStatus represents the observed state of a Workspace.
type WorkspaceStatus struct {
	commonv1.ManagedStatus `json:",inline"`
	Error                  *string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true

// A Workspace of OpenTofu Configuration.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Namespaced
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceList contains a list of Workspace
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}
