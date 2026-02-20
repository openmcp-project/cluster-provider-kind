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
	"encoding/base64"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	groupName               = "kind.clusters.openmcp.cloud"
	managedByNameLabel      = groupName + "/managed-by-name"
	managedByNamespaceLabel = groupName + "/managed-by-namespace"
	kindRole                = "Role"

	refreshTokenPercentage = 0.8

	reasonKindClusterInteractionError = "KindClusterInteractionError"
	reasonInternalError               = "InternalError"
	reasonInvalidReference            = "InvalidReference"
	reasonOIDCRequest                 = "OIDCBasedAccessRequest"
	reasonNotResponsible              = "NotResponsible"
)

var (
	errNotSupported                       = errors.New("not supported")
	defaultRequestedTokenValidityDuration = 30 * 24 * time.Hour // 30 days
)

type reconcileResult = ctrlutils.ReconcileResult[*clustersv1alpha1.AccessRequest]

// ClientProvider creates a client to connect to the cluster that the AccessRequest belongs to
type ClientProvider interface {
	CreateClient(kubeconfig string) (client.Client, *rest.Config, error)
}

// DefaultClientProvider creates a client for production use
var DefaultClientProvider ClientProvider = clientProviderImpl{}

type clientProviderImpl struct{}

// CreateClient implements [ClientProvider].
func (c clientProviderImpl) CreateClient(kubeconfig string) (client.Client, *rest.Config, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, nil, err
	}
	cl, err := client.New(restCfg, client.Options{})
	if err != nil {
		return nil, nil, err
	}
	return cl, restCfg, nil
}

// AccessRequestReconciler reconciles a AccessRequest object
type AccessRequestReconciler struct {
	ProviderName string
	client.Client
	Scheme          *runtime.Scheme
	ClusterProvider kind.Provider
	ClientProvider  ClientProvider
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AccessRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile")
	defer log.Info("Done")
	rr := r.reconcile(ctx, req)
	return ctrlutils.NewOpenMCPStatusUpdaterBuilder[*clustersv1alpha1.AccessRequest]().
		WithNestedStruct("Status").
		WithPhaseUpdateFunc(func(_ *clustersv1alpha1.AccessRequest, rr ctrlutils.ReconcileResult[*clustersv1alpha1.AccessRequest]) (string, error) {
			if rr.ReconcileError != nil || rr.Object == nil {
				return clustersv1alpha1.REQUEST_PENDING, nil
			}
			return clustersv1alpha1.REQUEST_GRANTED, nil
		}).
		Build().
		UpdateStatus(ctx, r.Client, rr)
}

func (r *AccessRequestReconciler) reconcile(ctx context.Context, req ctrl.Request) reconcileResult {
	log := logf.FromContext(ctx)
	ar := &clustersv1alpha1.AccessRequest{}
	if err := r.Get(ctx, req.NamespacedName, ar); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcileResult{}
		}
		return reconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("unable to get resource '%s' from cluster: %w", req.String(), err), reasonKindClusterInteractionError)}
	}

	if !libutils.IsClusterProviderResponsibleForAccessRequest(ar, r.ProviderName) {
		log.Info("ClusterProvider is not responsible for this AccessRequest, skipping reconciliation")
		return reconcileResult{}
	}

	rr := reconcileResult{
		Object:     ar,
		OldObject:  ar.DeepCopy(),
		Conditions: []metav1.Condition{},
	}

	if ar.Spec.ClusterRef == nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("AccessRequest %q/%q has no Cluster reference", ar.Namespace, ar.Name), reasonInvalidReference)
		return rr
	}

	clusterRef := types.NamespacedName{Name: ar.Spec.ClusterRef.Name, Namespace: ar.Spec.ClusterRef.Namespace}
	cluster := &clustersv1alpha1.Cluster{}
	if err := r.Get(ctx, clusterRef, cluster); err != nil && !apierrors.IsNotFound(err) {
		// TODO: report event or status condition?
		rr.ReconcileError = errutils.WithReason(err, reasonInvalidReference)
		return rr
	} else if !isClusterProviderResponsible(cluster) { // TODO: should be refactored
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("ClusterProfile '%s' is not supported by kind controller", cluster.Spec.Profile), reasonNotResponsible)
		return rr
	}

	// handle deletion
	if !ar.DeletionTimestamp.IsZero() {
		r.handleDelete(ctx, cluster, &rr)
	} else {
		r.handleCreateOrUpdate(ctx, cluster, &rr)
	}
	return rr
}

func (r *AccessRequestReconciler) handleCreateOrUpdate(ctx context.Context, cluster *clustersv1alpha1.Cluster, rr *reconcileResult) {
	if controllerutil.AddFinalizer(rr.Object, Finalizer) {
		if err := r.Update(ctx, rr.Object); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error patching finalizer on resource: %w", err), reasonKindClusterInteractionError)
			return
		}
	}
	name := kindName(cluster)
	kubeconfigStr, err := r.ClusterProvider.KubeConfig(name, false)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(err, reasonKindClusterInteractionError)
		return
	}
	cl, restCfg, err := r.ClientProvider.CreateClient(kubeconfigStr)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(err, reasonKindClusterInteractionError)
		return
	}
	ar := rr.Object
	if ar.Spec.OIDC != nil {
		rr.ReconcileError = errutils.WithReason(errNotSupported, reasonOIDCRequest)
		return
	}
	if ar.Spec.Token != nil && tokenRefreshRequired(ctx, cl, rr) {
		keep := r.reconcileTokenAccess(ctx, cl, restCfg, rr)
		if rr.ReconcileError != nil {
			return
		}
		if rerr := r.cleanupResources(ctx, cl, keep, managedResourcesLabels(ar)); rerr != nil {
			rr.ReconcileError = rerr
			return
		}
	}
}

func (r *AccessRequestReconciler) handleDelete(ctx context.Context, cluster *clustersv1alpha1.Cluster, rr *reconcileResult) {
	name := kindName(cluster)
	kubeconfigStr, err := r.ClusterProvider.KubeConfig(name, false)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(err, reasonKindClusterInteractionError)
	}
	cl, _, err := r.ClientProvider.CreateClient(kubeconfigStr)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(err, reasonKindClusterInteractionError)
	}
	if rerr := r.cleanupResources(ctx, cl, nil, managedResourcesLabels(rr.Object)); rerr != nil {
		rr.ReconcileError = rerr
	}
	// remove finalizer - Secret will automatically get deleted because of OwnerReference
	controllerutil.RemoveFinalizer(rr.Object, Finalizer)
	if err := r.Update(ctx, rr.Object); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error patching finalizer on resource: %w", err), reasonKindClusterInteractionError)
	}
	rr.Object = nil
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

	rbgvk := rbacv1.SchemeGroupVersion.WithKind("RoleBindingList")
	rgvk := rbacv1.SchemeGroupVersion.WithKind("RoleList")
	crbgvk := rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBindingList")
	crgvk := rbacv1.SchemeGroupVersion.WithKind("ClusterRoleList")
	sagvk := corev1.SchemeGroupVersion.WithKind("ServiceAccountList")

	resourceCleaners := []resourceCleaner{
		newResrouceCleaner[*rbacv1.RoleBinding](c, rbgvk, selector, keep),
		newResrouceCleaner[*rbacv1.Role](c, rgvk, selector, keep),
		newResrouceCleaner[*rbacv1.ClusterRoleBinding](c, crbgvk, selector, keep),
		newResrouceCleaner[*rbacv1.ClusterRole](c, crgvk, selector, keep),
		newResrouceCleaner[*corev1.ServiceAccount](c, sagvk, selector, keep),
	}
	for _, cleaner := range resourceCleaners {
		if err := cleaner.cleanup(ctx); err != nil {
			return err
		}
	}
	return nil
}

type resourceCleaner interface {
	cleanup(ctx context.Context) errutils.ReasonableError
}

type resourceCleanerImpl[T client.Object] struct {
	c        client.Client
	selector client.MatchingLabels
	keep     []client.Object
	ulist    *unstructured.UnstructuredList
}

func newResrouceCleaner[T client.Object](c client.Client, gvk schema.GroupVersionKind, selector client.MatchingLabels, keep []client.Object) resourceCleanerImpl[T] {
	list := unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)
	return resourceCleanerImpl[T]{
		c:        c,
		selector: selector,
		keep:     keep,
		ulist:    &list,
	}
}

func (r resourceCleanerImpl[T]) cleanup(ctx context.Context) errutils.ReasonableError {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Cleaning up", "kind", r.ulist.GetKind())
	errs := errutils.NewReasonableErrorList()

	if err := r.c.List(ctx, r.ulist, r.selector); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("error listing (%s): %w", r.ulist.GetKind(), err), reasonKindClusterInteractionError))
		return errs.Aggregate()
	}
	for _, item := range r.ulist.Items {
		keepThis := false
		for _, k := range r.keep {
			_, isType := k.(T)
			if k.GetName() == item.GetName() && k.GetNamespace() == item.GetNamespace() && isType {
				log.Debug("Keeping object", "kind", item.GetKind(), "resourceName", item.GetName(), "resourceNamespace", item.GetNamespace())
				keepThis = true
				break
			}
		}
		if keepThis {
			continue
		}
		log.Debug("Deleting object", "kind", item.GetKind(), "resourceName", item.GetName(), "resourceNamespace", item.GetNamespace())
		if err := r.c.Delete(ctx, &item); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug("object not found", "kind", item.GetKind(), "resourceName", item.GetName(), "resourceNamespace", item.GetNamespace())
			} else {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting object (%s) '%s/%s': %w", item.GetKind(), item.GetNamespace(), item.GetName(), err), reasonKindClusterInteractionError))
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

// reconcileTokenAccess creates a service account token that reflects the requested cluster access
// this includes reconciliation of the service account, the related (cluster) roles and (cluster) bindings in the cluster the access request is for
// and eventually creating a corresponding secret that holds the prepared kubeconfig in the platform cluster
func (r *AccessRequestReconciler) reconcileTokenAccess(ctx context.Context, c client.Client, cfg *rest.Config, rr *reconcileResult) []client.Object {
	log := logging.FromContextOrPanic(ctx)
	log.Info("reconcile token access")

	// ensure namespace
	_, err := clusteraccess.EnsureNamespace(ctx, c, AccessRequestServiceAccountNamespace())
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("create namespace %s failed: %w", AccessRequestServiceAccountNamespace(), err), reasonKindClusterInteractionError)
		return nil
	}

	ar := rr.Object
	// ensure service account
	name := ctrlutils.K8sNameUUIDUnsafe(Environment(), ProviderName(), ar.Namespace, ar.Name)
	sa, err := clusteraccess.EnsureServiceAccount(ctx, c, name, AccessRequestServiceAccountNamespace(), pairs.MapToPairs(managedResourcesLabels(ar))...)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("create service account %s/%s failed: %w", AccessRequestServiceAccountNamespace(), name, err), reasonKindClusterInteractionError)
		return nil
	}

	permObjs, errlist := reconcileRequestedPermissions(ctx, c, sa, ar)
	if err := errlist.Aggregate(); err != nil {
		rr.ReconcileError = err
		return nil
	}
	bindObjs, errlist := reconcileRequestedRoleBindings(ctx, c, sa, ar)
	if err := errlist.Aggregate(); err != nil {
		rr.ReconcileError = err
		return nil
	}
	keep := slices.Concat(permObjs, bindObjs)
	keep = append(keep, sa)

	// generate token
	token, err := clusteraccess.CreateTokenForServiceAccount(ctx, c, sa, &defaultRequestedTokenValidityDuration)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("request service account token failed: %w", err), reasonKindClusterInteractionError)
		return nil
	}
	rr.Result.RequeueAfter = time.Until(clusteraccess.ComputeTokenRenewalTimeWithRatio(token.CreationTimestamp, token.ExpirationTimestamp, refreshTokenPercentage))

	// create kubeconfig
	kcfg, err := clusteraccess.CreateTokenKubeconfig(ProviderName(), cfg.Host, cfg.CAData, token.Token)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("create token kubeconfig failed: %w", err), reasonInternalError)
		return nil
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
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("create/update kubeconfig secret failed: %w", err), reasonKindClusterInteractionError)
		return nil
	}

	ar.Status.SecretRef = &commonapi.LocalObjectReference{}
	ar.Status.SecretRef.Name = s.Name
	ar.Status.Phase = clustersv1alpha1.REQUEST_GRANTED

	return keep
}

func reconcileRequestedPermissions(ctx context.Context, c client.Client, sa *corev1.ServiceAccount, ar *clustersv1alpha1.AccessRequest) ([]client.Object, errutils.ReasonableErrorList) {
	log := logging.FromContextOrPanic(ctx)
	// ensure roles + bindings
	keep := []client.Object{}
	errlist := errutils.NewReasonableErrorList()
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
				errlist.Append(errutils.WithReason(fmt.Errorf("role (binding) error: %w", err), reasonKindClusterInteractionError))
				continue
			}
			keep = append(keep, r, rb)
		} else {
			// ensure cluster role + binding
			log.Debug("Ensuring ClusterRole and ClusterRoleBinding", "roleName", roleName)
			crb, cr, err := clusteraccess.EnsureClusterRoleAndBinding(ctx, c, roleName, subjects, permission.Rules, expectedLabels...)
			if err != nil {
				errlist.Append(errutils.WithReason(fmt.Errorf("cluster role (binding) error: %w", err), reasonKindClusterInteractionError))
				continue
			}
			keep = append(keep, cr, crb)
		}
	}
	return keep, *errlist
}

func reconcileRequestedRoleBindings(ctx context.Context, c client.Client, sa *corev1.ServiceAccount, ar *clustersv1alpha1.AccessRequest) ([]client.Object, errutils.ReasonableErrorList) {
	keep := []client.Object{}
	errlist := errutils.NewReasonableErrorList()
	expectedLabels := pairs.MapToPairs(managedResourcesLabels(ar))
	subjects := []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: sa.Namespace}}
	// ensure ServiceAccount is bound to (Cluster)Roles
	for i, roleRef := range ar.Spec.Token.RoleRefs {
		roleBindingName := fmt.Sprintf("openmcp:roleref:%s:%d", ctrlutils.K8sNameUUIDUnsafe(Environment(), ProviderName(), ar.Namespace, ar.Name), i)
		if roleRef.Kind == kindRole {
			// Role
			rb, err := clusteraccess.EnsureRoleBinding(ctx, c, roleBindingName, roleRef.Namespace, roleRef.Name, subjects, expectedLabels...)
			if err != nil {
				errlist.Append(errutils.WithReason(fmt.Errorf("role binding error: %w", err), reasonKindClusterInteractionError))
				continue
			}
			keep = append(keep, rb)
		} else {
			// ClusterRole
			crb, err := clusteraccess.EnsureClusterRoleBinding(ctx, c, roleBindingName, roleRef.Name, subjects, expectedLabels...)
			if err != nil {
				errlist.Append(errutils.WithReason(fmt.Errorf("cluster role binding error: %w", err), reasonKindClusterInteractionError))
				continue
			}
			keep = append(keep, crb)
		}
	}
	return keep, *errlist
}

func defaultSecretName(ar *clustersv1alpha1.AccessRequest) string {
	suffix := ".kubeconfig"
	return ctrlutils.ShortenToXCharactersUnsafe(ar.Name, ctrlutils.K8sMaxNameLength-len(suffix)) + suffix
}

func tokenRefreshRequired(ctx context.Context, c client.Client, rr *reconcileResult) bool {
	ar := rr.Object
	if ar.Status.Phase != clustersv1alpha1.REQUEST_GRANTED {
		return true
	}
	s := &corev1.Secret{}
	if err := c.Get(ctx, ctrlutils.ObjectKey(ar.Status.SecretRef.Name, ar.Namespace), s); err != nil {
		if !apierrors.IsNotFound(err) {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error getting secret '%s/%s': %w", ar.Namespace, ar.Status.SecretRef.Name, err), reasonKindClusterInteractionError)
			return false
		}
		s = nil
	}
	if s == nil {
		return true
	}
	creationTimestamp := base64.StdEncoding.EncodeToString(s.Data[clustersv1alpha1.SecretKeyCreationTimestamp])
	expirationTimestamp := base64.StdEncoding.EncodeToString(s.Data[clustersv1alpha1.SecretKeyExpirationTimestamp])
	if creationTimestamp != "" && expirationTimestamp != "" {
		tmp, err := strconv.ParseInt(creationTimestamp, 10, 64)
		if err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error parsing creation timestamp from secret '%s/%s': %w", s.Namespace, s.Name, err), reasonInternalError)
			return false
		}
		createdAt := time.Unix(tmp, 0)
		tmp, err = strconv.ParseInt(expirationTimestamp, 10, 64)
		if err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error parsing expiration timestamp from secret '%s/%s': %w", s.Namespace, s.Name, err), reasonInternalError)
			return false
		}
		expiredAt := time.Unix(tmp, 0)
		tokenRenewalTime := createdAt.Add(time.Duration(float64(expiredAt.Sub(createdAt)) * refreshTokenPercentage))
		if time.Now().Before(tokenRenewalTime) {
			// the request is granted, the secret still exists and the token is still valid - nothing to do
			rr.Result.RequeueAfter = time.Until(tokenRenewalTime)
			return false
		}
	}
	return true
}
