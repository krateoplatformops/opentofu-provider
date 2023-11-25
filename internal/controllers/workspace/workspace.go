package workspace

import (
	"context"

	commonv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	workspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"

	"github.com/krateoplatformops/opentofu-provider/internal/clients/opentofu"
)

const (
	tfPath   = "tofu"
	tfDir    = "/tf"
	tfMain   = "main.tf"
	tfConfig = "crossplane-provider-config.tf"
)

type tfclient interface {
	Init(ctx context.Context, o ...opentofu.InitOption) error
	Workspace(ctx context.Context, name string) error
	Outputs(ctx context.Context) ([]opentofu.Output, error)
	Resources(ctx context.Context) ([]string, error)
	Diff(ctx context.Context, o ...opentofu.Option) (bool, error)
	Apply(ctx context.Context, o ...opentofu.Option) error
	Destroy(ctx context.Context, o ...opentofu.Option) error
	DeleteCurrentWorkspace(ctx context.Context) error
}

const (
	errNotWorkspace       = "managed resource is not a Workspace custom resource"
	errTrackPCUsage       = "cannot track ProviderConfig usage"
	errGetConnectorConfig = "cannot get ConnectorConfig"
	errGetCreds           = "cannot get credentials"

	errMkdir           = "cannot make OpenTofu configuration directory"
	errRemoteModule    = "cannot get remote OpenTofu module"
	errSetGitCredDir   = "cannot set GIT_CRED_DIR environment variable"
	errWriteCreds      = "cannot write OpenTofu credentials"
	errWriteGitCreds   = "cannot write .git-credentials to /tmp dir"
	errWriteConfig     = "cannot write OpenTofu configuration " + tfConfig
	errWriteMain       = "cannot write OpenTofu configuration " + tfMain
	errInit            = "cannot initialize OpenTofu configuration"
	errWorkspace       = "cannot select OpenTofu workspace"
	errResources       = "cannot list OpenTofu resources"
	errDiff            = "cannot diff (i.e. plan) OpenTofu configuration"
	errOutputs         = "cannot list OpenTofu outputs"
	errOptions         = "cannot determine OpenTofu options"
	errApply           = "cannot apply OpenTofu configuration"
	errDestroy         = "cannot destroy OpenTofu configuration"
	errVarFile         = "cannot get tfvars"
	errDeleteWorkspace = "cannot delete OpenTofu workspace"
	errEnvVar          = "cannot get environment variables"

	gitCredentialsFilename = ".git-credentials"

	reasonCreated = "CreatedExternalResource"
	reasonDeleted = "DeletedExternalResource"
)

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotWorkspace)
	}

	cr.Status.SetConditions(commonv1.Available())

	if cr.Status.AtProvider == nil {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	}

	return reconciler.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: true,
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	return nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}

	spec := cr.Spec.Workspace
	o, err := e.options(ctx, &spec)
	if err != nil {
		return errors.Wrap(err, errOptions)
	}

	o = append(o, opentofu.WithArgs(spec.ApplyArgs))
	if err := e.tf.Apply(ctx, o...); err != nil {
		return errors.Wrap(err, errApply)
	}

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonCreated,
		"opentofu apply '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	op, err := e.tf.Outputs(ctx)
	if err != nil {
		return errors.Wrap(err, errOutputs)
	}
	obs := generateWorkspaceObservation(op)
	cr.Status.AtProvider = &obs
	cr.Status.SetConditions(commonv1.Available())

	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	return nil // noop
}

func (c *external) options(ctx context.Context, p *workspacev1alpha1.WorkspaceParameters) ([]opentofu.Option, error) {
	o := make([]opentofu.Option, 0, len(p.Vars)+len(p.VarFiles)+len(p.DestroyArgs)+len(p.ApplyArgs)+len(p.PlanArgs))

	for _, v := range p.Vars {
		o = append(o, opentofu.WithVar(v.Key, v.Value))
	}

	for _, vf := range p.VarFiles {
		fmt := opentofu.HCL
		if vf.Format == &workspacev1alpha1.VarFileFormatJSON {
			fmt = opentofu.JSON
		}

		switch vf.Source {
		case workspacev1alpha1.VarFileSourceConfigMapKey:
			cm := &corev1.ConfigMap{}
			r := vf.ConfigMapKeyReference
			nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
			if err := c.kube.Get(ctx, nn, cm); err != nil {
				return nil, errors.Wrap(err, errVarFile)
			}
			o = append(o, opentofu.WithVarFile([]byte(cm.Data[r.Key]), fmt))

		case workspacev1alpha1.VarFileSourceSecretKey:
			s := &corev1.Secret{}
			r := vf.SecretKeyReference
			nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
			if err := c.kube.Get(ctx, nn, s); err != nil {
				return nil, errors.Wrap(err, errVarFile)
			}
			o = append(o, opentofu.WithVarFile(s.Data[r.Key], fmt))
		}
	}

	return o, nil
}

// generateWorkspaceObservation is used to produce v1alpha1.WorkspaceObservation from
// workspace_type.Workspace.
func generateWorkspaceObservation(op []opentofu.Output) workspacev1alpha1.WorkspaceObservation {
	wo := workspacev1alpha1.WorkspaceObservation{
		Outputs: make(map[string]string, len(op)),
	}
	for _, o := range op {
		if !o.Sensitive {
			if o.Type == opentofu.OutputTypeString {
				wo.Outputs[o.Name] = o.StringValue()
			} else if j, err := o.JSONValue(); err == nil {
				wo.Outputs[o.Name] = string(j)
			}
		}
	}
	return wo
}
