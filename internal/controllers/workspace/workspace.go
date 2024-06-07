package workspace

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	commonv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/meta"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	workspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"

	"github.com/krateoplatformops/opentofu-provider/internal/clients/opentofu"
)

const observingReason = "Observing"

var observingCondition = commonv1.Condition{
	Type:               commonv1.TypeReady,
	Status:             metav1.ConditionTrue,
	LastTransitionTime: metav1.Now(),
	Reason:             observingReason,
}

const (
	errNotWorkspace       = "managed resource is not a Workspace custom resource"
	errTrackPCUsage       = "cannot track ProviderConfig usage"
	errGetConnectorConfig = "cannot get ConnectorConfig"
	errGetCreds           = "cannot get credentials"

	reasonUpdated = "UpdatedExternalResource"
	reasonCreated = "CreatedExternalResource"
	reasonDeleted = "DeletedExternalResource"
)

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotWorkspace)
	}

	e.log.Info("Observing", "name", cr.GetName())

	// fmt.Println("Conditions - ", cr.Status.Conditions)
	if cr.Status.GetCondition(commonv1.TypeSynced).Status == metav1.ConditionUnknown || cr.Status.GetCondition(commonv1.TypeReady).Reason == commonv1.ReasonUnavailable {
		e.log.Debug("Creating condition", "name", cr.GetName())
		return reconciler.ExternalObservation{
			ResourceExists:   false,
			ResourceUpToDate: false,
		}, nil
	}

	if cr.GetCondition(commonv1.Deleting().Type).Reason == commonv1.ReasonDeleting {
		e.log.Info("Deleting condition", "name", cr.GetName())
		cr.SetDeletionTimestamp(nil)
	}

	// if cr.Status.Error != nil {
	// 	if !cr.GetDeletionTimestamp().IsZero() {
	// 		return reconciler.ExternalObservation{
	// 			ResourceExists:   false,
	// 			ResourceUpToDate: true,
	// 		}, nil
	// 	}
	// 	return reconciler.ExternalObservation{}, fmt.Errorf("failed to observe: %s", *cr.Status.Error)
	// }

	cond := cr.Status.GetCondition(commonv1.TypeReady)
	if string(cond.Reason) == observingReason || cond.Reason == commonv1.ReasonDeleting {
		job, err := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitPlan), cr.GetNamespace())
		if apierrors.IsNotFound(err) || job == nil {
			if cond.Reason != commonv1.ReasonDeleting {
				e.log.Debug("Setting available condition - job not found - Deleting condition - Observing condition")
				cr.SetConditions(commonv1.Available())
				cr.Status.Error = nil
				return reconciler.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: true,
				}, e.kube.Status().Update(ctx, cr)
			}
		} else if cond.Reason == commonv1.ReasonDeleting && job.Status.Active != 1 {
			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}
		} else if job.Status.Succeeded == 1 {
			jobInfo, err := opentofu.GetJobInfo(ctx, e.kube, job.GetName(), job.GetNamespace())
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}
			completedPod := jobInfo.GetSuccededPod()
			if completedPod == nil {
				return reconciler.ExternalObservation{}, fmt.Errorf("job failed: %s", *jobInfo.Errs)
			}

			exitCode := completedPod.Status.ContainerStatuses[0].State.Terminated.ExitCode

			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}

			if exitCode == 0 {
				if opentofu.ClassifyPlanPodLog(*jobInfo.Logs) {
					e.log.Info("Workspace is up to date", "name", cr.GetName())
					cr.SetConditions(commonv1.Available())
					cr.Status.Error = nil
					return reconciler.ExternalObservation{
						ResourceExists:   true,
						ResourceUpToDate: true,
					}, e.kube.Status().Update(ctx, cr)
				}

				e.log.Info("Workspace is not up to date", "name", cr.GetName())
				return reconciler.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				}, nil
			}

			cr.SetConditions(commonv1.Unavailable())
			return reconciler.ExternalObservation{}, fmt.Errorf("job failed: %s", *jobInfo.Errs)
		} else if job.Status.Failed > 0 {
			cr.SetConditions(commonv1.Unavailable())
			jobInfo, err := opentofu.GetJobInfo(ctx, e.kube, job.GetName(), job.GetNamespace())
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}

			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}

			strErr := fmt.Errorf("job failed: %s", *jobInfo.Errs).Error()

			cr.Status.Error = &strErr

			err = e.kube.Status().Update(ctx, cr)
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}

			return reconciler.ExternalObservation{}, fmt.Errorf("job failed: %s", *jobInfo.Errs)
		} else {
			cr.SetConditions(observingCondition)
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, nil
		}
	}

	// Check if the workspace is up to date
	cond = cr.Status.GetCondition(commonv1.Available().Type)
	if cond.Reason == commonv1.ReasonAvailable {
		e.log.Debug("Checking if workspace is up to date", "name", cr.GetName())
		job, err := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitPlan), cr.GetNamespace())
		if apierrors.IsNotFound(err) || job == nil {

			err := opentofu.Run(ctx, e.kube, *cr.DeepCopy(), opentofu.InitPlan)
			if err != nil {
				return reconciler.ExternalObservation{}, fmt.Errorf("failed to plan: %w", err)
			}
			e.log.Debug("Plan job created", "name", opentofu.JobNamer(cr.ObjectMeta, opentofu.InitPlan))

			cr.SetConditions(observingCondition)

			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, nil
		}
	}

	cond = cr.Status.GetCondition(commonv1.Available().Type)
	if cond.Reason == commonv1.ReasonCreating || cond.Reason == commonv1.ReasonDeleting {
		job, err := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitApply), cr.GetNamespace())
		if apierrors.IsNotFound(err) || job == nil {
			if cond.Reason != commonv1.ReasonDeleting {
				e.log.Debug("Setting available condition - job not found - Deleting condition - Creating condition")
				cr.SetConditions(commonv1.Available())
				cr.Status.Error = nil
				e.log.Debug("Creating condition", "name", cr.GetName(), "reason", cond.Reason)
				return reconciler.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: true,
				}, e.kube.Status().Update(ctx, cr)
			}

		} else if cond.Reason == commonv1.ReasonDeleting && job.Status.Active != 1 {
			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}
		} else if job.Status.Succeeded == 1 {
			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}
			e.log.Debug("Setting available condition - job succeeded")
			cr.SetConditions(commonv1.Available())
			cr.Status.Error = nil
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, e.kube.Status().Update(ctx, cr)
		} else if job.Status.Failed > 0 {
			cr.SetConditions(commonv1.Unavailable())
			jobInfo, err := opentofu.GetJobInfo(ctx, e.kube, job.GetName(), job.GetNamespace())
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}

			deletePropagation := metav1.DeletePropagationForeground
			if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
				return reconciler.ExternalObservation{}, err
			}

			strErr := fmt.Errorf("job failed: %s", *jobInfo.Errs).Error()

			cr.Status.Error = &strErr

			err = e.kube.Status().Update(ctx, cr)
			if err != nil {
				return reconciler.ExternalObservation{}, err
			}

			return reconciler.ExternalObservation{}, fmt.Errorf("job failed: %s", *jobInfo.Errs)
		} else {
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: true,
			}, nil
		}
	}

	// fmt.Println("Checking if workspace is up to date - Deleting condition")
	cond = cr.Status.GetCondition(commonv1.Deleting().Type)
	if cond.Reason == commonv1.ReasonDeleting {
		cr.SetDeletionTimestamp(nil)
		job, err := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitDestroy), cr.GetNamespace())
		if err != nil && !apierrors.IsNotFound(err) {
			return reconciler.ExternalObservation{}, err
		}
		planJob, planErr := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitPlan), cr.GetNamespace())
		applyJob, applyErr := opentofu.GetJob(ctx, e.kube, opentofu.JobNamer(cr.ObjectMeta, opentofu.InitApply), cr.GetNamespace())
		if (apierrors.IsNotFound(planErr) || planJob == nil) && (apierrors.IsNotFound(applyErr) || applyJob == nil) && (job == nil) {
			e.log.Debug("Running destroy job", "name", cr.GetName())
			err := opentofu.Run(ctx, e.kube, *cr.DeepCopy(), opentofu.InitDestroy)
			if err != nil {
				return reconciler.ExternalObservation{}, fmt.Errorf("failed to destroy: %w", err)
			}

			e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonDeleted,
				"opentofu destroy started for '%s (id: %s)' success", cr.GetName(), cr.GetUID())

			return reconciler.ExternalObservation{}, nil
		}

		// if apierrors.IsNotFound(err) || job == nil {
		// 	return reconciler.ExternalObservation{
		// 		ResourceExists:   false,
		// 		ResourceUpToDate: true,
		// 	}, nil
		// }

		if job != nil {
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
			if job.Status.Failed > 0 {
				cr.SetConditions(commonv1.Unavailable())
				jobInfo, err := opentofu.GetJobInfo(ctx, e.kube, job.GetName(), job.GetNamespace())
				if err != nil {
					return reconciler.ExternalObservation{}, err
				}

				deletePropagation := metav1.DeletePropagationForeground
				if err = e.kube.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &deletePropagation}); err != nil {
					return reconciler.ExternalObservation{}, err
				}

				strErr := fmt.Errorf("job failed: %s", *jobInfo.Errs).Error()

				cr.Status.Error = &strErr

				err = e.kube.Status().Update(ctx, cr)
				if err != nil {
					return reconciler.ExternalObservation{}, err
				}

				return reconciler.ExternalObservation{}, fmt.Errorf("job failed: %s", *jobInfo.Errs)
			}
		}

		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

	cond = cr.Status.GetCondition(commonv1.Unavailable().Type)
	if cond.Reason == commonv1.ReasonUnavailable {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

	e.log.Debug("Setting available condition - default")
	cr.Status.SetConditions(commonv1.Available())

	return reconciler.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: true,
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*workspacev1alpha1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}

	if cr.Status.GetCondition(commonv1.Creating().Type).Reason == commonv1.ReasonDeleting {
		return nil
	}

	e.log.Info("Creating", "name", cr.GetName())

	err := opentofu.Run(ctx, e.kube, *cr.DeepCopy(), opentofu.InitApply)
	if err != nil {
		return fmt.Errorf("failed to apply: %w", err)
	}

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonCreated,
		"opentofu apply '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	cr.Status.SetConditions(commonv1.Creating())

	return e.kube.Status().Update(ctx, cr)
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

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonUpdated,
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

	// err := opentofu.Run(ctx, e.kube, *cr.DeepCopy(), opentofu.InitDestroy)
	// if err != nil {
	// 	return fmt.Errorf("failed to destroy: %w", err)
	// }

	e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonDeleted,
		"opentofu destroy scheduled '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	// e.recorder.Eventf(cr, corev1.EventTypeNormal, reasonDeleted,
	// 	"opentofu destroy '%s (id: %s)' success", cr.GetName(), cr.GetUID())

	cr.Status.SetConditions(commonv1.Deleting())

	return nil //e.kube.Status().Update(ctx, cr)
}
