/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/openmcp-project/controller-utils/pkg/clusteraccess"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/pairs"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"

	"github.com/openmcp-project/cluster-provider-kind/pkg/kind"
)

const (
	groupName                           = "kind.clusters.openmcp.cloud"
	managedByNameLabel                  = groupName + "/managed-by-name"
	managedByNamespaceLabel             = groupName + "/managed-by-namespace"
	kindRole                            = "Role"
	kindClusterRole                     = "ClusterRole"
	kindRoleBinding                     = "RoleBinding"
	kindClusterRoleBinding              = "ClusterRoleBinding"
	reasonKindClusterInteractionProblem = "KindClusterInteractionProblem"
	reasonInternalError                 = "InternalError"
)

var (
	errFailedToGetReferencedCluster       = errors.New("failed to fetch referenced cluster")
	defaultRequestedTokenValidityDuration = 30 * 24 * time.Hour // 30 days
)

// AccessRequestReconciler reconciles a AccessRequest object
type AccessRequestReconciler struct {
	ProviderName string
	client.Client
	Scheme   *runtime.Scheme
	Provider kind.Provider
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AccessRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile")
	defer log.Info("Done")

	ar := &clustersv1alpha1.AccessRequest{}
	if err := r.Get(ctx, req.NamespacedName, ar); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !libutils.IsClusterProviderResponsibleForAccessRequest(ar, r.ProviderName) {
		log.Info("ClusterProvider is not responsible for this AccessRequest, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	defer r.Status().Update(ctx, ar) //nolint:errcheck

	if ar.Spec.ClusterRef == nil {
		return ctrl.Result{}, fmt.Errorf("AccessRequest %q/%q has no Cluster reference", ar.Namespace, ar.Name)
	}

	clusterRef := types.NamespacedName{Name: ar.Spec.ClusterRef.Name, Namespace: ar.Spec.ClusterRef.Namespace}
	cluster := &clustersv1alpha1.Cluster{}
	if err := r.Get(ctx, clusterRef, cluster); err != nil && !apierrors.IsNotFound(err) {
		// TODO: report event or status condition?
		return ctrl.Result{}, errors.Join(err, errFailedToGetReferencedCluster)

	} else if !isClusterProviderResponsible(cluster) { // TODO: should be refactored
		return ctrl.Result{}, fmt.Errorf("ClusterProfile '%s' is not supported by kind controller", cluster.Spec.Profile)
	}

	// handle deletion
	if !ar.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, ar, cluster)
	}

	return r.handleCreateOrUpdate(ctx, ar, cluster)
}

func (r *AccessRequestReconciler) handleCreateOrUpdate(ctx context.Context, ar *clustersv1alpha1.AccessRequest, cluster *clustersv1alpha1.Cluster) (ctrl.Result, error) {
	if controllerutil.AddFinalizer(ar, Finalizer) {
		if err := r.Update(ctx, ar); err != nil {
			return ctrl.Result{}, err
		}
	}

	name := kindName(cluster)
	kubeconfigStr, err := r.Provider.KubeConfig(name, false)
	// kubeconfigStr, err := r.Provider.KubeConfig(name, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigStr))
	if err != nil {
		return ctrl.Result{}, err
	}

	cl, err := client.New(restCfg, client.Options{})
	if err != nil {
		return ctrl.Result{}, err
	}

	var keep []client.Object
	if ar.Spec.Token != nil {
		keep, err = r.renewToken(ctx, cl, restCfg, ar)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if rerr := r.cleanupResources(ctx, cl, keep, managedResourcesLabels(ar)); rerr != nil {
		return ctrl.Result{}, rerr
	}

	return ctrl.Result{}, nil
}

func (r *AccessRequestReconciler) handleDelete(ctx context.Context, ar *clustersv1alpha1.AccessRequest, cluster *clustersv1alpha1.Cluster) (ctrl.Result, error) {
	// remove finalizer - Secret will automatically get deleted because of OwnerReference
	controllerutil.RemoveFinalizer(ar, Finalizer)
	if err := r.Update(ctx, ar); err != nil {
		return ctrl.Result{}, err
	}
	name := kindName(cluster)
	// kubeconfigStr, err := r.Provider.KubeConfig(name, false)
	kubeconfigStr, err := r.Provider.KubeConfig(name, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigStr))
	if err != nil {
		return ctrl.Result{}, err
	}

	cl, err := client.New(restCfg, client.Options{})
	if err != nil {
		return ctrl.Result{}, err
	}

	if rerr := r.cleanupResources(ctx, cl, nil, managedResourcesLabels(ar)); rerr != nil {
		return ctrl.Result{}, rerr
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccessRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.AccessRequest{}).
		WithEventFilter(
			predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return libutils.IsClusterProviderResponsibleForAccessRequest(obj.(*clustersv1alpha1.AccessRequest), r.ProviderName)
			}),
		).
		Named("accessrequest").
		Complete(r)
}

func (r *AccessRequestReconciler) cleanupResources(ctx context.Context, c client.Client, keep []client.Object, labels map[string]string) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Cleaning up resources that are not required anymore")

	if len(labels) == 0 {
		return errutils.WithReason(fmt.Errorf("no labels provided for cleanup"), reasonInternalError)
	}
	selector := client.MatchingLabels(labels)

	if err := r.cleanupRoleBindings(ctx, c, selector, keep); err != nil {
		return err
	}
	if err := r.cleanupClusterRoleBindings(ctx, c, selector, keep); err != nil {
		return err
	}
	if err := r.cleanupRoles(ctx, c, selector, keep); err != nil {
		return err
	}
	if err := r.cleanupClusterRoles(ctx, c, selector, keep); err != nil {
		return err
	}
	if err := r.cleanupServiceAccounts(ctx, c, selector, keep); err != nil {
		return err
	}

	return nil
}

func (r *AccessRequestReconciler) cleanupRoleBindings(ctx context.Context, c client.Client, selector client.MatchingLabels, keep []client.Object) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Cleaning up RoleBindings")

	errs := errutils.NewReasonableErrorList()
	rbs := &rbacv1.RoleBindingList{}
	if err := c.List(ctx, rbs, selector); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("error listing RoleBindings: %w", err), reasonKindClusterInteractionProblem))
		return errs.Aggregate()
	}
	for _, rb := range rbs.Items {
		keepThis := false
		for _, k := range keep {
			if k.GetName() == rb.Name && k.GetNamespace() == rb.Namespace && k.GetObjectKind().GroupVersionKind().Kind == kindRoleBinding {
				log.Debug("Keeping RoleBinding", "resourceName", rb.Name, "resourceNamespace", rb.Namespace)
				keepThis = true
				break
			}
		}
		if keepThis {
			continue
		}
		log.Debug("Deleting RoleBinding", "resourceName", rb.Name, "resourceNamespace", rb.Namespace)
		if err := c.Delete(ctx, &rb); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug("RoleBinding not found", "resourceName", rb.Name, "resourceNamespace", rb.Namespace)
			} else {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting RoleBinding '%s/%s': %w", rb.Namespace, rb.Name, err), reasonKindClusterInteractionProblem))
			}
		}
	}
	return errs.Aggregate()
}

func (r *AccessRequestReconciler) cleanupClusterRoleBindings(ctx context.Context, c client.Client, selector client.MatchingLabels, keep []client.Object) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Cleaning up ClusterRoleBindings")

	errs := errutils.NewReasonableErrorList()
	crbs := &rbacv1.ClusterRoleBindingList{}
	if err := c.List(ctx, crbs, selector); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("error listing ClusterRoleBindings: %w", err), reasonKindClusterInteractionProblem))
		return errs.Aggregate()
	}
	for _, crb := range crbs.Items {
		keepThis := false
		for _, k := range keep {
			if k.GetName() == crb.Name && k.GetObjectKind().GroupVersionKind().Kind == kindClusterRoleBinding {
				log.Debug("Keeping ClusterRoleBinding", "resourceName", crb.Name)
				keepThis = true
				break
			}
		}
		if keepThis {
			continue
		}
		log.Debug("Deleting ClusterRoleBinding", "resourceName", crb.Name)
		if err := c.Delete(ctx, &crb); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug("ClusterRoleBinding not found", "resourceName", crb.Name)
			} else {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting ClusterRoleBinding '%s': %w", crb.Name, err), reasonKindClusterInteractionProblem))
			}
		}
	}
	return errs.Aggregate()
}

func (r *AccessRequestReconciler) cleanupRoles(ctx context.Context, c client.Client, selector client.MatchingLabels, keep []client.Object) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Cleaning up Roles")

	errs := errutils.NewReasonableErrorList()
	roles := &rbacv1.RoleList{}
	if err := c.List(ctx, roles, selector); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("error listing Roles: %w", err), reasonKindClusterInteractionProblem))
		return errs.Aggregate()
	}
	for _, role := range roles.Items {
		keepThis := false
		for _, k := range keep {
			if k.GetName() == role.Name && k.GetNamespace() == role.Namespace && k.GetObjectKind().GroupVersionKind().Kind == kindRole {
				log.Debug("Keeping Role", "resourceName", role.Name, "resourceNamespace", role.Namespace)
				keepThis = true
				break
			}
		}
		if keepThis {
			continue
		}
		log.Debug("Deleting Role", "resourceName", role.Name, "resourceNamespace", role.Namespace)
		if err := c.Delete(ctx, &role); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug("Role not found", "resourceName", role.Name, "resourceNamespace", role.Namespace)
			} else {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting Role '%s/%s': %w", role.Namespace, role.Name, err), reasonKindClusterInteractionProblem))
			}
		}
	}
	return errs.Aggregate()
}

func (r *AccessRequestReconciler) cleanupClusterRoles(ctx context.Context, c client.Client, selector client.MatchingLabels, keep []client.Object) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Cleaning up ClusterRoles")

	errs := errutils.NewReasonableErrorList()
	crs := &rbacv1.ClusterRoleList{}
	if err := c.List(ctx, crs, selector); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("error listing ClusterRoles: %w", err), reasonKindClusterInteractionProblem))
		return errs.Aggregate()
	}
	for _, cr := range crs.Items {
		keepThis := false
		for _, k := range keep {
			if k.GetName() == cr.Name && k.GetObjectKind().GroupVersionKind().Kind == kindClusterRole {
				log.Debug("Keeping ClusterRole", "resourceName", cr.Name)
				keepThis = true
				break
			}
		}
		if keepThis {
			continue
		}
		log.Debug("Deleting ClusterRole", "resourceName", cr.Name)
		if err := c.Delete(ctx, &cr); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug("ClusterRole not found", "resourceName", cr.Name)
			} else {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting ClusterRole '%s': %w", cr.Name, err), reasonKindClusterInteractionProblem))
			}
		}
	}
	return errs.Aggregate()
}

func (r *AccessRequestReconciler) cleanupServiceAccounts(ctx context.Context, c client.Client, selector client.MatchingLabels, keep []client.Object) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Cleaning up ServiceAccounts")

	errs := errutils.NewReasonableErrorList()
	sas := &corev1.ServiceAccountList{}
	if err := c.List(ctx, sas, selector); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("error listing ServiceAccounts: %w", err), reasonKindClusterInteractionProblem))
		return errs.Aggregate()
	}
	for _, sa := range sas.Items {
		keepThis := false
		for _, k := range keep {
			if k.GetName() == sa.Name && k.GetNamespace() == sa.Namespace && k.GetObjectKind().GroupVersionKind().Kind == "ServiceAccount" {
				log.Debug("Keeping ServiceAccount", "resourceName", sa.Name, "resourceNamespace", sa.Namespace)
				keepThis = true
				break
			}
		}
		if keepThis {
			continue
		}
		log.Debug("Deleting ServiceAccount", "resourceName", sa.Name, "resourceNamespace", sa.Namespace)
		if err := c.Delete(ctx, &sa); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug("ServiceAccount not found", "resourceName", sa.Name, "resourceNamespace", sa.Namespace)
			} else {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting ServiceAccount '%s/%s': %w", sa.Namespace, sa.Name, err), reasonKindClusterInteractionProblem))
			}
		}
	}
	return errs.Aggregate()
}

func managedResourcesLabels(ac *clustersv1alpha1.AccessRequest) map[string]string {
	return map[string]string{
		managedByNameLabel:      ac.Name,
		managedByNamespaceLabel: ac.Namespace,
	}
}

// renewToken creates a service account token that reflects the requested cluster access
// this includes reconciliation of the service account, the related (cluster) roles and (cluster) bindings in the cluster the access request is for
// and eventually creating a corresponding secret that holds the prepared kubeconfig in the platform cluster
func (r *AccessRequestReconciler) renewToken(ctx context.Context, c client.Client, cfg *rest.Config, ar *clustersv1alpha1.AccessRequest) ([]client.Object, error) {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Creating new service account token")

	// ensure namespace
	_, err := clusteraccess.EnsureNamespace(ctx, c, AccessRequestServiceAccountNamespace())
	if err != nil {
		return nil, err
	}

	// ensure service account
	name := ctrlutils.K8sNameUUIDUnsafe(Environment(), ProviderName(), ar.Namespace, ar.Name)
	sa, err := clusteraccess.EnsureServiceAccount(ctx, c, name, AccessRequestServiceAccountNamespace(), pairs.MapToPairs(managedResourcesLabels(ar))...)
	if err != nil {
		return nil, err
	}
	if sa.GroupVersionKind().Kind == "" {
		sa.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))
	}

	permissions, err := reconcilePermissions(ctx, c, sa, ar)
	if err != nil {
		return permissions, err
	}
	roles, err := reconcileRoles(ctx, c, sa, ar)
	if err != nil {
		return permissions, err
	}
	keep := slices.Concat(permissions, roles)
	keep = append(keep, sa)

	// generate token
	token, err := clusteraccess.CreateTokenForServiceAccount(ctx, c, sa, &defaultRequestedTokenValidityDuration)
	if err != nil {
		return nil, err
	}

	// create kubeconfig
	kcfg, err := clusteraccess.CreateTokenKubeconfig(ProviderName(), cfg.Host, cfg.CAData, token.Token)
	if err != nil {
		return nil, err
	}

	// create/update secret
	sm := resources.NewSecretMutatorWithStringData(defaultSecretName(ar), ar.Namespace, map[string]string{
		clustersv1alpha1.SecretKeyKubeconfig:          string(kcfg),
		clustersv1alpha1.SecretKeyExpirationTimestamp: strconv.FormatInt(token.ExpirationTimestamp.Unix(), 10),
		clustersv1alpha1.SecretKeyCreationTimestamp:   strconv.FormatInt(token.CreationTimestamp.Unix(), 10),
	}, corev1.SecretTypeOpaque)
	sm.MetadataMutator().WithOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: clustersv1alpha1.GroupVersion.String(),
			Kind:       "AccessRequest",
			Name:       ar.Name,
			UID:        ar.UID,
			Controller: ptr.To(true),
		},
	})
	s := sm.Empty()
	if err := resources.CreateOrUpdateResource(ctx, r.Client, sm); err != nil {
		return nil, err
	}

	ar.Status.SecretRef = &commonapi.LocalObjectReference{}
	ar.Status.SecretRef.Name = s.Name
	ar.Status.Phase = clustersv1alpha1.REQUEST_GRANTED

	return keep, nil
}

func reconcilePermissions(ctx context.Context, c client.Client, sa *corev1.ServiceAccount, ar *clustersv1alpha1.AccessRequest) ([]client.Object, error) {
	log := logging.FromContextOrPanic(ctx)
	// ensure roles + bindings
	keep := []client.Object{}
	expectedLabels := pairs.MapToPairs(managedResourcesLabels(ar))
	subjects := []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: sa.Namespace}}
	for i, permission := range ar.Spec.Token.Permissions {
		roleName := permission.Name
		if roleName == "" {
			roleName = fmt.Sprintf("openmcp:permission:%s:%d", ctrlutils.K8sNameUUIDUnsafe(Environment(), ProviderName(), ar.Namespace, ar.Name), i)
		}
		if permission.Namespace != "" {
			// ensure role + binding
			log.Debug("Ensuring Role and RoleBinding", "roleName", roleName, "namespace", permission.Namespace)
			rb, r, err := clusteraccess.EnsureRoleAndBinding(ctx, c, roleName, permission.Namespace, subjects, permission.Rules, expectedLabels...)
			if err != nil {
				log.Error(err, "error ensuring role")
				continue
			}
			if rb.GroupVersionKind().Kind == "" {
				rb.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind(kindRoleBinding))
			}
			keep = append(keep, rb)
			if r.GroupVersionKind().Kind == "" {
				r.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind(kindRole))
			}
			keep = append(keep, r)
		} else {
			// ensure cluster role + binding
			log.Debug("Ensuring ClusterRole and ClusterRoleBinding", "roleName", roleName)
			crb, cr, err := clusteraccess.EnsureClusterRoleAndBinding(ctx, c, roleName, subjects, permission.Rules, expectedLabels...)
			if err != nil {
				log.Error(err, "error ensuring cluster role")
				continue
			}
			if crb.GroupVersionKind().Kind == "" {
				crb.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind(kindClusterRoleBinding))
			}
			keep = append(keep, crb)
			if cr.GroupVersionKind().Kind == "" {
				cr.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind(kindClusterRole))
			}
			keep = append(keep, cr)
		}
	}
	return keep, nil
}

func reconcileRoles(ctx context.Context, c client.Client, sa *corev1.ServiceAccount, ar *clustersv1alpha1.AccessRequest) ([]client.Object, error) {
	keep := []client.Object{}
	expectedLabels := pairs.MapToPairs(managedResourcesLabels(ar))
	subjects := []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: sa.Namespace}}
	// ensure ServiceAccount is bound to (Cluster)Roles
	for i, roleRef := range ar.Spec.Token.RoleRefs {
		roleBindingName := fmt.Sprintf("openmcp:roleref:%s:%d", ctrlutils.K8sNameUUIDUnsafe(Environment(), ProviderName(), ar.Namespace, ar.Name), i)
		if roleRef.Kind == kindRole {
			// Role
			rb, err := clusteraccess.EnsureRoleBinding(ctx, c, roleBindingName, roleRef.Namespace, roleRef.Name, subjects, expectedLabels...)
			if err != nil {
				return nil, err
			}
			if rb.GroupVersionKind().Kind == "" {
				rb.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind(kindRoleBinding))
			}
			keep = append(keep, rb)
		} else {
			// ClusterRole
			crb, err := clusteraccess.EnsureClusterRoleBinding(ctx, c, roleBindingName, roleRef.Name, subjects, expectedLabels...)
			if err != nil {
				return nil, err
			}
			if crb.GroupVersionKind().Kind == "" {
				crb.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind(kindClusterRoleBinding))
			}
			keep = append(keep, crb)
		}
	}
	return keep, nil
}

func defaultSecretName(ar *clustersv1alpha1.AccessRequest) string {
	suffix := ".kubeconfig"
	return ctrlutils.ShortenToXCharactersUnsafe(ar.Name, ctrlutils.K8sMaxNameLength-len(suffix)) + suffix
}
