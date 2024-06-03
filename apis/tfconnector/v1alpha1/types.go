package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// // A EnvVarSource specifies the source of a Terraform env vars.
// // +kubebuilder:validation:Enum=ConfigMapKey;SecretKey
// type EnvVarSource string

// // Vars file sources.
// const (
// 	EnvVarSourceConfigMapKey EnvVarSource = "ConfigMapKey"
// 	EnvVarSourceSecretKey    EnvVarSource = "SecretKey"
// )

// EnvVar Opentofu CLI env vars.
// https://opentofu.org/docs/cli/cloud/settings/#environment-variables
type EnvVar struct {
	// Name of the env var (eg. https://opentofu.org/docs/cli/cloud/settings/#environment-variables)
	Name string `json:"name"`
	// // A ConfigMap key containing the env var value.
	// // +optional
	// ConfigMapKeyReference *rtv1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// // A Secret key containing the env var value.
	// rtv1.env
}

// type BackendCredentials struct {
// 	// Hostname of the Cloud Backend. (eg app.terraform.io)
// 	Hostname *string `json:"hostname"`
// 	// SecretRef reference to the secret containing the credentials.
// 	SecretRef rtv1.SecretKeySelector `json:"secretRef"`
// }

type ProviderCredentials struct {
	// // CredFile where to save credentials file.
	// CredFilename string `json:"credFilename"`

	// EnvironmentVars to set for the provider.
	EnvVars []corev1.EnvFromSource `json:"envVars"`
}

type TFConnectorSpec struct {
	// // BackendCredentials required to authenticate. eg. Terraform Cloud
	// BackendCredentials []BackendCredentials `json:"backendCredentials"`

	// EnvVars environment variables for OpenTofu cli.
	// +optional
	EnvVars []corev1.EnvFromSource `json:"envVars,omitempty"`
	// Credentials required to authenticate.
	// +optional
	ProvidersCredentials ProviderCredentials `json:"providersCredentials"`

	// GitCredentials required to authenticate. The name of the env var MUST be GIT_CREDENTIALS.
	// eg. kubectl create secret generic git-creds --from-literal=GIT_CREDENTIALS=<TOKEN>
	// +optional
	GitCredentials *corev1.EnvFromSource `json:"gitCredentials,omitempty"`

	// Configuration that should be injected into all workspaces that use
	// this provider config, expressed as inline HCL. This can be used to
	// automatically inject Terraform provider configuration blocks.
	// +optional
	// Configuration *string `json:"configuration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={krateo,opentofu}
type TFConnector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TFConnectorSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// TFConnectorList contains a list of TFConnector
type TFConnectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TFConnector `json:"items"`
}
