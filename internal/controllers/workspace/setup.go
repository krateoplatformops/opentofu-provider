package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	worspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"
	"github.com/krateoplatformops/opentofu-provider/internal/clients/opentofu"
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/meta"
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
	initTf func(dir string, verbose bool) tfclient
}

type external struct {
	log      logging.Logger
	recorder record.EventRecorder
	fs       afero.Afero
	dir      string
	tf       tfclient
	kube     client.Client
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
			initTf: func(dir string, verbose bool) tfclient {
				return opentofu.Harness{
					Path:    tfPath,
					Dir:     dir,
					Verbose: verbose,
				}
			},
		}),
		reconciler.WithPollInterval(o.PollInterval),
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
		return nil, fmt.Errorf("failed to create workspace directory %s : %w, %s", tfDir, err, errMkdir)
	}

	tf := c.initTf(dir, meta.IsVerbose(cr))

	return &external{
		log:      c.log,
		recorder: c.recorder,
		fs:       c.fs,
		dir:      dir,
		tf:       tf,
		kube:     c.kube,
	}, nil // errors.Wrap(tf.Workspace(ctx, meta.GetExternalName(cr)), errWorkspace)
}
