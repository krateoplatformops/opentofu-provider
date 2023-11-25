package apis

import (
	"k8s.io/apimachinery/pkg/runtime"

	connectorconfigv1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/tfconnector/v1alpha1"
	workspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes,
		workspacev1alpha1.SchemeBuilder.AddToScheme,
		connectorconfigv1alpha1.SchemeBuilder.AddToScheme,
	)
}

// AddToSchemes may be used to add all resources defined in the project to a Scheme
var AddToSchemes runtime.SchemeBuilder

// AddToScheme adds all Resources to the Scheme
func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
