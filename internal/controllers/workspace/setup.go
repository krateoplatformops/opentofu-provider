package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-getter"
	worspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"
	"github.com/krateoplatformops/opentofu-provider/internal/clients/opentofu"
	"github.com/krateoplatformops/opentofu-provider/internal/controllers/resolvers"
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type connector struct {
	kube client.Client

	log      logging.Logger
	recorder record.EventRecorder

	fs     afero.Afero
	initTf func(dir string) tfclient
}

type external struct {
	log      logging.Logger
	recorder record.EventRecorder

	tf   tfclient
	kube client.Client
}

// Setup adds a controller that reconciles Token managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := reconciler.ControllerName(worspacev1alpha1.WorkspaceGroupKind)

	log := o.Logger.WithValues("controller", name)

	recorder := mgr.GetEventRecorderFor(name)
	fs := afero.Afero{Fs: afero.NewOsFs()}

	r := reconciler.NewReconciler(mgr,
		resource.ManagedKind(worspacev1alpha1.WorkspaceGroupVersionKind),
		reconciler.WithExternalConnecter(&connector{
			kube:     mgr.GetClient(),
			log:      log,
			recorder: recorder,
			fs:       fs,
			initTf:   func(dir string) tfclient { return opentofu.Harness{Path: tfPath, Dir: dir} },
		}),
		reconciler.WithLogger(log),
		reconciler.WithRecorder(event.NewAPIRecorder(recorder)))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&worspacev1alpha1.Workspace{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (reconciler.ExternalClient, error) {
	cr, ok := mg.(*worspacev1alpha1.Workspace)
	if !ok {
		return nil, errors.New(errNotWorkspace)
	}

	dir := filepath.Join(tfDir, string(cr.GetUID()))
	if err := c.fs.MkdirAll(dir, 0700); resource.Ignore(os.IsExist, err) != nil {
		return nil, errors.Wrap(err, errMkdir)
	}

	connectorConfig, err := resolvers.ResolveTFConnector(ctx, c.kube, cr.Spec.ConnectorConfigRef)
	if err != nil {
		return nil, errors.Wrap(err, errGetConnectorConfig)
	}

	for _, v := range connectorConfig.EnvVars {
		os.Setenv(v.Name, v.Value)
	}

	// create TF_TOKEN_<hostname> env vars. Note: hostname on vars is underscore separated. eg. app.terraform.io -> app_terraform_io
	for _, v := range connectorConfig.BackenedCreds {
		hostname := strings.ReplaceAll(v.Name, ".", "_")
		varName := "TF_TOKEN_" + hostname
		os.Setenv(varName, v.Value)
	}

	switch cr.Spec.Workspace.Source {
	case worspacev1alpha1.ModuleSourceRemote:
		// Workaround of https://github.com/hashicorp/go-getter/issues/114
		if err := c.fs.RemoveAll(dir); err != nil {
			return nil, errors.Wrap(err, errRemoteModule)
		}

		client := getter.Client{
			Src: cr.Spec.Workspace.Module,
			Dst: dir,
			Pwd: dir,

			Mode: getter.ClientModeAny,
		}
		err := client.Get()
		if err != nil {
			return nil, errors.Wrap(err, errRemoteModule)
		}

	case worspacev1alpha1.ModuleSourceInline:
		if err := c.fs.WriteFile(filepath.Join(dir, tfMain), []byte(cr.Spec.Workspace.Module), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteMain)
		}
	}

	if len(cr.Spec.Workspace.Entrypoint) > 0 {
		entrypoint := strings.ReplaceAll(cr.Spec.Workspace.Entrypoint, "../", "")
		dir = filepath.Join(dir, entrypoint)
	}

	if connectorConfig.Configuration != nil {
		if err := c.fs.WriteFile(filepath.Join(dir, tfConfig), []byte(*connectorConfig.Configuration), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteConfig)
		}
	}
	for _, v := range connectorConfig.ProviderCreds {
		if err := c.fs.WriteFile(filepath.Join(dir, v.CredFilename), []byte(v.Value), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteConfig)
		}
	}

	tf := c.initTf(dir)
	o := make([]opentofu.InitOption, 0, len(cr.Spec.Workspace.InitArgs))
	o = append(o, opentofu.WithInitArgs(cr.Spec.Workspace.InitArgs))
	if err := tf.Init(ctx, o...); err != nil {
		return nil, errors.Wrap(err, errInit)
	}

	return &external{
		log:      c.log,
		recorder: c.recorder,
		tf:       tf,
		kube:     c.kube,
	}, nil // errors.Wrap(tf.Workspace(ctx, meta.GetExternalName(cr)), errWorkspace)
}
