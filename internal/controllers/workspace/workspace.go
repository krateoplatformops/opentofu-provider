package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-getter"
	commonv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	workspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"

	"github.com/krateoplatformops/opentofu-provider/internal/clients/opentofu"
	"github.com/krateoplatformops/opentofu-provider/internal/controllers/resolvers"
	"github.com/krateoplatformops/opentofu-provider/internal/controllers/tools"
)

const (
	tfPath   = "tofu"
	tfDir    = "/tmp/tf"
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

	cond := cr.Status.GetCondition(commonv1.Deleting().Type)
	if cond.Reason == commonv1.ReasonDeleting {
		return reconciler.ExternalObservation{
			ResourceExists:   false,
			ResourceUpToDate: true,
		}, nil
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

	ctx, cancelFunc := tools.SetContextDeadlineForCLI(ctx)
	defer cancelFunc()

	e.log.Info("Update", "name", cr.GetName())

	err := e.initRepo(ctx, *cr)
	if err != nil {
		return fmt.Errorf("failed to init repo: %w", err)
	}

	initOpts := make([]opentofu.InitOption, 0, len(cr.Spec.Workspace.InitArgs))
	initOpts = append(initOpts, opentofu.WithInitArgs(cr.Spec.Workspace.InitArgs))
	if err := e.tf.Init(ctx, initOpts...); err != nil {
		return errors.Wrap(err, errInit)
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

	return e.kube.Status().Update(ctx, cr)
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}
	ctx, cancelFunc := tools.SetContextDeadlineForCLI(ctx)
	defer cancelFunc()

	e.log.Info("Delete", "name", cr.GetName())

	err := e.initRepo(ctx, *cr)
	if err != nil {
		return fmt.Errorf("failed to init repo: %w", err)
	}

	initOpts := make([]opentofu.InitOption, 0, len(cr.Spec.Workspace.InitArgs))
	initOpts = append(initOpts, opentofu.WithInitArgs(cr.Spec.Workspace.InitArgs))
	if err := e.tf.Init(ctx, initOpts...); err != nil {
		return errors.Wrap(err, errInit)
	}

	spec := cr.Spec.Workspace
	o, err := e.options(ctx, &spec)
	if err != nil {
		return errors.Wrap(err, errOptions)
	}

	o = append(o, opentofu.WithArgs(spec.ApplyArgs))
	if err := e.tf.Destroy(ctx, o...); err != nil {
		return errors.Wrap(err, errDestroy)
	}

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonDeleted,
		"opentofu destroy '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	cr.Status.SetConditions(commonv1.Deleting())

	return nil //e.kube.Status().Update(ctx, cr)
}

func (e *external) initRepo(ctx context.Context, cr workspacev1alpha1.Workspace) error {

	connectorConfig, err := resolvers.ResolveTFConnector(ctx, e.kube, cr.Spec.TFConnectorRef)
	if err != nil {
		return errors.Wrap(err, errGetConnectorConfig)
	}

	for _, v := range connectorConfig.EnvVars {
		os.Setenv(v.Name, v.Value)
	}

	// create TF_TOKEN_<hostname> env vars. Note: hostname on vars is underscore separated. eg. app.terraform.io -> app_terraform_io
	for _, v := range connectorConfig.BackenedCreds {
		hostname := strings.ReplaceAll(v.Name, ".", "_")
		varName := "TF_TOKEN_" + hostname
		err := os.Setenv(varName, v.Value)
		if err != nil {
			return fmt.Errorf("failed to set env var %s: %w", varName, err)
		}
	}

	switch cr.Spec.Workspace.Source {
	case workspacev1alpha1.ModuleSourceRemote:
		// Workaround of https://github.com/hashicorp/go-getter/issues/114
		if err := e.fs.RemoveAll(e.dir); err != nil {
			return errors.Wrap(err, errRemoteModule)
		}

		client := getter.Client{
			Src: cr.Spec.Workspace.Module,
			Dst: e.dir,
			Pwd: e.dir,

			Mode: getter.ClientModeAny,
		}
		err := client.Get()
		if err != nil {
			return errors.Wrap(err, errRemoteModule)
		}

	case workspacev1alpha1.ModuleSourceInline:
		if err := e.fs.WriteFile(filepath.Join(e.dir, tfMain), []byte(cr.Spec.Workspace.Module), 0600); err != nil {
			return errors.Wrap(err, errWriteMain)
		}
	}

	if len(cr.Spec.Workspace.Entrypoint) > 0 {
		entrypoint := strings.ReplaceAll(cr.Spec.Workspace.Entrypoint, "../", "")
		e.dir = filepath.Join(e.dir, entrypoint)
	}

	if connectorConfig.Configuration != nil {
		if err := e.fs.WriteFile(filepath.Join(e.dir, tfConfig), []byte(*connectorConfig.Configuration), 0600); err != nil {
			return errors.Wrap(err, errWriteConfig)
		}
	}
	for _, v := range connectorConfig.ProviderCreds {
		if err := e.fs.WriteFile(filepath.Join(e.dir, v.CredFilename), []byte(v.Value), 0600); err != nil {
			return errors.Wrap(err, errWriteConfig)
		}
	}

	return nil
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
