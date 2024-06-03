package resolvers

import (
	"context"

	connectorconfigs "github.com/krateoplatformops/opentofu-provider/apis/tfconnector/v1alpha1"
	rtv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// import (
// 	"context"
// 	"fmt"

// 	connectorconfigs "github.com/krateoplatformops/opentofu-provider/apis/tfconnector/v1alpha1"
// 	rtv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
// 	"github.com/krateoplatformops/provider-runtime/pkg/helpers"
// 	"github.com/krateoplatformops/provider-runtime/pkg/resource"
// 	"github.com/pkg/errors"
// 	"sigs.k8s.io/controller-runtime/pkg/client"

// 	corev1 "k8s.io/api/core/v1"
// 	"k8s.io/apimachinery/pkg/types"
// )

// type EnvVars struct {
// 	// Name of the env var (example: TF_TOKEN_app_terraform_io)
// 	Name  string `json:"name"`
// 	Value string `json:"value"`
// }
// type Credentials struct {
// 	Name  string `json:"name"`
// 	Value string `json:"value"`
// }

// type ProviderCredentials struct {
// 	// CredFile where to save credentials file.
// 	CredFilename string `json:"credFilename"`

// 	Value string `json:"value"`
// }

// type ConfigOption struct {
// 	EnvVars       []EnvVars             `json:"envVars,omitempty"`
// 	ProviderCreds []ProviderCredentials `json:"credentials,omitempty"`
// 	BackenedCreds []Credentials         `json:"backendCredentials,omitempty"`
// 	Configuration *string               `json:"configuration,omitempty"`
// }

// func ResolveTFConnector(ctx context.Context, kube client.Client, ref *rtv1.Reference) (ConfigOption, error) {
// 	opts := ConfigOption{}

// 	cfg := connectorconfigs.TFConnector{}
// 	if ref == nil {
// 		return opts, fmt.Errorf("no %s referenced", cfg.Kind)
// 	}

// 	err := kube.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &cfg)
// 	if err != nil {
// 		return opts, errors.Wrapf(err, "cannot get %s connector config", ref.Name)
// 	}

// 	for _, cred := range cfg.Spec.ProvidersCredentials {
// 		sec := corev1.Secret{}
// 		err = kube.Get(ctx, types.NamespacedName{Namespace: cred.SecretRef.Namespace, Name: cred.SecretRef.Name}, &sec)
// 		if err != nil {
// 			return opts, errors.Wrapf(err, "cannot get %s secret", ref.Name)
// 		}

// 		credFileContent, err := resource.GetSecret(ctx, kube, cred.SecretRef.DeepCopy())
// 		if err != nil {
// 			return opts, err
// 		}

// 		opts.ProviderCreds = append(opts.ProviderCreds, ProviderCredentials{
// 			CredFilename: cred.CredFilename,
// 			Value:        string(credFileContent),
// 		})
// 	}

// 	for _, v := range cfg.Spec.BackendCredentials {
// 		sec := corev1.Secret{}
// 		err = kube.Get(ctx, types.NamespacedName{Namespace: v.SecretRef.Namespace, Name: v.SecretRef.Name}, &sec)
// 		if err != nil {
// 			return opts, errors.Wrapf(err, "cannot get %s secret", ref.Name)
// 		}

// 		token, err := resource.GetSecret(ctx, kube, v.SecretRef.DeepCopy())
// 		if err != nil {
// 			return opts, err
// 		}

// 		opts.BackenedCreds = append(opts.BackenedCreds, Credentials{
// 			Name:  helpers.String(v.Hostname),
// 			Value: string(token),
// 		})
// 	}

// 	for _, env := range cfg.Spec.EnvVars {
// 		value, err := resource.GetConfigMapValue(ctx, kube, env.ConfigMapKeyReference)
// 		if err != nil {
// 			return opts, err
// 		}
// 		opts.EnvVars = append(opts.EnvVars, EnvVars{
// 			Name:  env.Name,
// 			Value: value,
// 		})
// 	}

// 	opts.Configuration = cfg.Spec.Configuration

// 	return opts, nil
// }

type ConfigOption struct {
}

func ResolveTFConnector(ctx context.Context, kube client.Client, ref *rtv1.Reference) (*connectorconfigs.TFConnector, error) {
	cfg := connectorconfigs.TFConnector{}

	err := kube.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
