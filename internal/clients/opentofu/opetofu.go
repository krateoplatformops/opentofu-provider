package opentofu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	retry "github.com/avast/retry-go/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	workspacev1alpha1 "github.com/krateoplatformops/opentofu-provider/apis/workspace/v1alpha1"
	"github.com/krateoplatformops/opentofu-provider/internal/controllers/resolvers"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientgo "k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Action string

const (
	InitApply   Action = "init-apply"
	InitDestroy Action = "init-destroy"
	InitPlan    Action = "init-plan"
)

const (
	opentofuImage = "ghcr.io/opentofu/opentofu:latest"
	gitImage      = "alpine/git:latest"
)

func int32Ptr(i int32) *int32 { return &i }

type JobRunner struct {
	Metadata metav1.ObjectMeta
	Pod      corev1.Pod
}

func (r *JobRunner) generatePVC() *corev1.PersistentVolumeClaim {
	// Create a new PVC
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Metadata.Name,
			Namespace: r.Metadata.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	return pvc
}

func (r *JobRunner) generateRole() *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Metadata.Name,
			Namespace: r.Metadata.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"*"},
			},
		},
	}
	return role
}

func (r *JobRunner) generateRoleBinding() *rbacv1.RoleBinding {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Metadata.Name,
			Namespace: r.Metadata.Namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.Metadata.Name,
				Namespace: r.Metadata.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     r.Metadata.Name,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	return roleBinding
}

func (r *JobRunner) generateServiceAccount() *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Metadata.Name,
			Namespace: r.Metadata.Namespace,
		},
	}
	return sa
}

func (r *JobRunner) generateJob() *batchv1.Job {
	pod := r.Pod.DeepCopy()
	// In a job, pod's can only have OnFailure or Never restart policies
	if pod.Spec.RestartPolicy == corev1.RestartPolicyAlways || pod.Spec.RestartPolicy == corev1.RestartPolicyOnFailure {
		pod.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
	} else {
		pod.Spec.RestartPolicy = corev1.RestartPolicyNever
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:         pod.Name,
			GenerateName: pod.GenerateName,
			Labels:       pod.Labels,
			Annotations:  pod.Annotations,
			Namespace:    pod.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				Spec: pod.Spec,
			},
		},
	}
	return &job

}

func (a Action) GetCMDs() []string {
	switch a {
	case InitApply:
		return []string{
			"tofu init -no-color -input=false",
			"tofu apply -no-color -auto-approve -input=false",
		}
	case InitDestroy:
		return []string{
			"tofu init -no-color -input=false",
			"tofu destroy -no-color -auto-approve -input=false",
		}
	case InitPlan:
		return []string{
			"tofu init -no-color -input=false",
			"tofu plan -no-color -input=false",
		}
	default:
		return []string{}
	}
}

func (a Action) String() string {
	return string(a)
}

// OpenTofu often returns a summary of the error it encountered on a single
// line, prefixed with 'Error: '.
var tfError = regexp.MustCompile(`Error: (.+)\n`)

// Classify errors returned from the OpenTofu CLI by inspecting its stderr.
func classifyPodLog(str string) error {

	lines := bytes.Split([]byte(str), []byte("\n"))

	// If stderr contains multiple lines we try return the first thing that
	// looks like a summary of the error.
	if m := tfError.FindSubmatch([]byte(str)); len(lines) > 0 && len(m) > 1 {
		return errors.New(string(bytes.ToLower(m[1])))
	}

	// Failing that, try to return the first non-empty line.
	for _, line := range lines {
		if len(line) > 0 {
			return errors.New(string(bytes.ToLower(line)))
		}
	}

	return errors.New("unknown error")
}

func addOwnerRef(ctx context.Context, kube client.Client, sa *corev1.ServiceAccount, role *rbacv1.Role, roleBinding *rbacv1.RoleBinding, pvc *corev1.PersistentVolumeClaim, owRef metav1.OwnerReference) error {
	sa.OwnerReferences = append(sa.OwnerReferences, owRef)
	role.OwnerReferences = append(role.OwnerReferences, owRef)
	roleBinding.OwnerReferences = append(roleBinding.OwnerReferences, owRef)

	if err := kube.Update(ctx, sa); err != nil {
		return fmt.Errorf("failed to update service account: %w", err)
	}
	if err := kube.Update(ctx, role); err != nil {
		return fmt.Errorf("failed to update role: %w", err)
	}
	if err := kube.Update(ctx, roleBinding); err != nil {
		return fmt.Errorf("failed to update role binding: %w", err)
	}
	return nil
}

func GetJobLogs(ctx context.Context, kube client.Client, jobname, namespace string) (*string, *string, error) {
	restconfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, nil, err
	}
	clientraw, err := clientgo.NewForConfig(restconfig)
	if err != nil {
		return nil, nil, err
	}
	pods := corev1.PodList{}
	selector := labels.SelectorFromSet(labels.Set(map[string]string{"job-name": jobname}))
	err = kube.List(ctx, &pods, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: selector,
	})
	if err != nil {
		return nil, nil, err
	}

	errsbuf := []string{}
	var log *string

	for _, pod := range pods.Items {
		// if strings.Contains(pod.GetName(), jobname) {
		req := clientraw.CoreV1().Pods(namespace).GetLogs(pod.GetName(), &corev1.PodLogOptions{})
		podLogs, err := req.Stream(ctx)
		if err != nil {
			errsbuf = append(errsbuf, fmt.Sprintf("%s: %s", pod.GetName(), err.Error()))
			err = nil
			continue
		}
		defer podLogs.Close()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to copy pod logs: %w", err)
		}

		str := buf.String()

		log = &str

		errsbuf = append(errsbuf, fmt.Sprintf("%s: %s", pod.GetName(), classifyPodLog(str).Error()))
	}

	joined := strings.Join(errsbuf, "\n")
	return log, &joined, nil
}

func GetJob(ctx context.Context, kube client.Client, name, namespace string) (*batchv1.Job, error) {
	job := batchv1.Job{}
	err := kube.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &job, &client.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func JobNamer(meta metav1.ObjectMeta, action Action) string {
	return fmt.Sprintf("%s-opentofu-%s", meta.GetName(), action.String())
}

func Run(ctx context.Context, kube client.Client, cr workspacev1alpha1.Workspace, action Action) error {
	cfg, err := resolvers.ResolveTFConnector(ctx, kube, cr.Spec.TFConnectorRef)
	if err != nil {
		return fmt.Errorf("failed to resolve TFConnector: %w", err)
	}

	var envs []corev1.EnvFromSource
	envs = append(envs, cfg.Spec.EnvVars...)
	envs = append(envs, cfg.Spec.ProvidersCredentials.EnvVars...)

	var initEnvs []corev1.EnvFromSource
	if cfg.Spec.GitCredentials != nil {
		initEnvs = append(initEnvs, *cfg.Spec.GitCredentials)
	}

	cmds := action.GetCMDs()

	cloneCommand := fmt.Sprintf("git clone -c credential.helper='!f() { echo username=author; echo \"password=$GIT_CREDENTIALS\"; };f' %s workspace", cr.Spec.Workspace.Module)

	name := JobNamer(cr.ObjectMeta, action)
	runner := JobRunner{
		Metadata: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.ObjectMeta.Namespace,
		},
	}

	pvc := runner.generatePVC()

	// Need to set owner reference for the PVC and for service account, role and role binding
	sa := runner.generateServiceAccount()
	role := runner.generateRole()
	roleBinding := runner.generateRoleBinding()
	if err := InstallServiceAccount(ctx, kube, sa); err != nil {
		return fmt.Errorf("failed to create service account: %w", err)
	}
	if err := InstallRole(ctx, kube, role); err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}
	if err := InstallRoleBinding(ctx, kube, roleBinding); err != nil {
		return fmt.Errorf("failed to create role binding: %w", err)
	}

	runner.Pod = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.ObjectMeta.Namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:       name,
					Image:      opentofuImage,
					WorkingDir: "/mnt/workspace",
					Command:    []string{"sh", "-c"},
					Args:       []string{strings.Join(cmds, " && ")},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      pvc.GetName(),
							MountPath: "/mnt",
						},
					},
					EnvFrom: envs,
				},
			},
			ServiceAccountName: sa.GetName(),
			InitContainers: []corev1.Container{
				{
					Name:       fmt.Sprintf("%s-init", name),
					Image:      gitImage,
					EnvFrom:    initEnvs,
					WorkingDir: "/mnt",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      pvc.GetName(),
							MountPath: "/mnt",
						},
					},
					Command: []string{"sh", "-c"},
					Args:    []string{cloneCommand},
					// Command: []string{"git", "clone", cr.Spec.Workspace.Module, "workspace"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: pvc.GetName(),
					VolumeSource: corev1.VolumeSource{
						Ephemeral: &corev1.EphemeralVolumeSource{
							VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{
								Spec: pvc.Spec,
							},
						},
					},
				},
			},
		},
	}

	job := runner.generateJob()

	// bjob, err := yaml.Marshal(job)
	// fmt.Println(string(bjob))
	// fmt.Println()

	// Create the job
	err = kube.Create(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	owRef := metav1.OwnerReference{
		APIVersion: batchv1.SchemeGroupVersion.String(),
		Kind:       "Job",
		Name:       job.GetName(),
		UID:        job.GetUID(),
	}
	if err := addOwnerRef(ctx, kube, sa, role, roleBinding, pvc, owRef); err != nil {
		return fmt.Errorf("failed to add owner reference: %w", err)
	}

	return nil
}

func InstallRole(ctx context.Context, kube client.Client, obj *rbacv1.Role) error {
	return retry.Do(
		func() error {
			tmp := rbacv1.Role{}
			err := kube.Get(ctx, client.ObjectKeyFromObject(obj), &tmp)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return kube.Create(ctx, obj)
				}

				return err
			}

			return nil
		},
	)
}

func InstallRoleBinding(ctx context.Context, kube client.Client, obj *rbacv1.RoleBinding) error {
	return retry.Do(
		func() error {
			tmp := rbacv1.RoleBinding{}
			err := kube.Get(ctx, client.ObjectKeyFromObject(obj), &tmp)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return kube.Create(ctx, obj)
				}

				return err
			}

			return nil
		},
	)
}

func InstallServiceAccount(ctx context.Context, kube client.Client, obj *corev1.ServiceAccount) error {
	return retry.Do(
		func() error {
			tmp := corev1.ServiceAccount{}
			err := kube.Get(ctx, client.ObjectKeyFromObject(obj), &tmp)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return kube.Create(ctx, obj)
				}

				return err
			}

			return nil
		},
	)
}

func InstallPVC(ctx context.Context, kube client.Client, obj *corev1.PersistentVolumeClaim) error {
	return retry.Do(
		func() error {
			tmp := corev1.PersistentVolumeClaim{}
			err := kube.Get(ctx, client.ObjectKeyFromObject(obj), &tmp)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return kube.Create(ctx, obj)
				}

				return err
			}

			return nil
		},
	)
}
