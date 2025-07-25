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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"

	"github.com/openmcp-project/cluster-provider-kind/pkg/kind"
)

var (
	errFailedToGetReferencedCluster = errors.New("failed to fetch referenced cluster")
)

// AccessRequestReconciler reconciles a AccessRequest object
type AccessRequestReconciler struct {
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

	ar.Status.Phase = clustersv1alpha1.AccessRequestPending

	defer r.Status().Update(ctx, ar) //nolint:errcheck

	clusterRef := types.NamespacedName{Name: ar.Spec.ClusterRef.Name, Namespace: ar.Namespace}
	cluster := &clustersv1alpha1.Cluster{}
	if err := r.Get(ctx, clusterRef, cluster); err != nil {
		return ctrl.Result{}, errors.Join(err, errFailedToGetReferencedCluster)
	}

	// Check if Cluster resource has the correct profile
	if cluster.Spec.Profile != profileKind {
		return ctrl.Result{}, fmt.Errorf("profile '%s' is not supported by kind controller", cluster.Spec.Profile)
	}

	// handle deletion
	if !ar.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, ar)
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
	kubeconfigStr, err := r.Provider.KubeConfig(name)
	if err != nil {
		return ctrl.Result{}, err
	}

	s := getSecretNamespacedName(ar)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Type = corev1.SecretTypeOpaque

		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data["kubeconfig"] = []byte(kubeconfigStr)
		return controllerutil.SetOwnerReference(ar, secret, r.Scheme)
	})

	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create or update secret for access request %q/%q: %w", ar.Namespace, ar.Name, err)
	}

	ar.Status.Phase = clustersv1alpha1.AccessRequestGranted
	ar.Status.SecretRef = &commonapi.ObjectReference{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}

	return ctrl.Result{}, nil
}

func (r *AccessRequestReconciler) handleDelete(ctx context.Context, ar *clustersv1alpha1.AccessRequest) (ctrl.Result, error) {
	// remove finalizer - Secret will automatically get deleted because of OwnerReference
	controllerutil.RemoveFinalizer(ar, Finalizer)
	if err := r.Update(ctx, ar); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func getSecretNamespacedName(ar *clustersv1alpha1.AccessRequest) types.NamespacedName {
	return types.NamespacedName{
		Name:      ar.Name + ".kubeconfig",
		Namespace: ar.Namespace,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccessRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.AccessRequest{}).
		Named("accessrequest").
		Complete(r)
}
