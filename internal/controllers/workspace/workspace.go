package workspace

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/meta"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"

	"github.com/krateoplatformops/opentofu-provider/internal/clients/opentofu"
)

const (
	errNotWorkspace       = "managed resource is not a Workspace custom resource"
	errTrackPCUsage       = "cannot track ProviderConfig usage"
	errGetConnectorConfig = "cannot get ConnectorConfig"
	errGetCreds           = "cannot get credentials"

	reasonCreated = "CreatedExternalResource"
	reasonDeleted = "DeletedExternalResource"
)

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotWorkspace)
	}

	e.log.Info("Observing", "name", cr.GetName())

	if cr.Status.Error != nil {
		if !cr.GetDeletionTimestamp().IsZero() {
			return reconciler.ExternalObservation{
				ResourceExists:   false,
				ResourceUpToDate: true,
			}, nil
		}
		return reconciler.ExternalObservation{}, fmt.Errorf("failed to observe: %s", *cr.Status.Error)
	}

	cond := cr.Status.GetCondition(commonv1.Deleting().Type)
	if cond.Reason == commonv1.ReasonDeleting {
		cr.SetDeletionTimestamp(nil)
		job, err := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitDestroy), cr.GetNamespace())
		if apierrors.IsNotFound(err) || job == nil {
			return reconciler.ExternalObservation{
				ResourceExists:   false,
				ResourceUpToDate: true,
			}, nil
		}
		if job.Status.Succeeded == 1 {
			cr.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}

			return reconciler.ExternalObservation{
				ResourceExists:   false,
				ResourceUpToDate: true,
			}, nil
		}
		return reconciler.ExternalObservation{}, nil
	}

	cond = cr.Status.GetCondition(commonv1.Available().Type)
	if cond.Reason == commonv1.ReasonCreating {
		job, err := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitApply), cr.GetNamespace())
		if apierrors.IsNotFound(err) || job == nil {
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, nil
		}
		if job.Status.Succeeded == 1 {
			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, nil
		}
		if job.Status.Failed > 0 {
			cr.SetConditions(commonv1.Unavailable())
			_, logsErr, err := opentofu.GetJobLogs(ctx, e.kube, job.GetName(), job.GetNamespace())
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}

			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}

			strErr := fmt.Errorf("job failed: %s", *logsErr).Error()

			cr.Status.Error = &strErr

			err = e.kube.Status().Update(ctx, cr)
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}

			return reconciler.ExternalObservation{}, fmt.Errorf("job failed: %s", *logsErr)
		}
		return reconciler.ExternalObservation{}, nil
	}
	cond = cr.Status.GetCondition(commonv1.Unavailable().Type)
	if cond.Reason == commonv1.ReasonUnavailable {
		return reconciler.ExternalObservation{}, nil
	}

	cr.Status.SetConditions(commonv1.Available())

	return reconciler.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: false,
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

	e.log.Info("Update", "name", cr.GetName())

	err := opentofu.Run(ctx, e.kube, *cr.DeepCopy(), opentofu.InitApply)
	if err != nil {
		return fmt.Errorf("failed to apply: %w", err)
	}

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonCreated,
		"opentofu apply '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	cr.Status.SetConditions(commonv1.Creating())

	return e.kube.Status().Update(ctx, cr)
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}

	if !meta.IsActionAllowed(cr, meta.ActionDelete) {
		return fmt.Errorf("delete action is not allowed")
	}

	e.log.Info("Delete", "name", cr.GetName())

	if cr.Status.Error != nil {
		e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonDeleted,
			"no need to destroy '%s (id: %s)' success", cr.GetName(), cr.GetUID())

		cr.Status.Error = nil

		cr.Status.SetConditions(commonv1.Deleting())
		return e.kube.Status().Update(ctx, cr)
	}

	err := opentofu.Run(ctx, e.kube, *cr.DeepCopy(), opentofu.InitDestroy)
	if err != nil {
		return fmt.Errorf("failed to destroy: %w", err)
	}

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonDeleted,
		"opentofu destroy '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	cr.Status.SetConditions(commonv1.Deleting())

	return nil //e.kube.Status().Update(ctx, cr)
}
