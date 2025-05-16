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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openmcp-project/cluster-provider-kind/api/v1alpha1"
	"github.com/openmcp-project/cluster-provider-kind/pkg/kind"
	"github.com/openmcp-project/cluster-provider-kind/pkg/metallb"
	"github.com/openmcp-project/cluster-provider-kind/pkg/smartrequeue"
)

var (
	Finalizer = v1alpha1.GroupVersion.Group + "/finalizer"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	RequeueStore *smartrequeue.Store
	Provider     kind.Provider
}

// +kubebuilder:rbac:groups=kind.clusters.openmcp.cloud,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kind.clusters.openmcp.cloud,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kind.clusters.openmcp.cloud,resources=clusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile")
	defer log.Info("Done")

	cluster := &v1alpha1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Always try to update the status
	defer r.Status().Update(ctx, cluster)

	ctx = smartrequeue.NewContext(ctx, r.RequeueStore.For(cluster))

	if !cluster.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, cluster)
	}

	return r.handleCreateOrUpdate(ctx, cluster)
}

func (r *ClusterReconciler) handleDelete(ctx context.Context, cluster *v1alpha1.Cluster) (ctrl.Result, error) {
	requeue := smartrequeue.FromContext(ctx)
	cluster.Status.Phase = v1alpha1.Terminating

	if !controllerutil.ContainsFinalizer(cluster, Finalizer) {
		// Nothing to do
		return ctrl.Result{}, nil
	}

	name := kindName(cluster)

	exists, err := r.Provider.ClusterExists(name)
	if err != nil {
		return requeue.Error(err)
	}

	if !exists {
		controllerutil.RemoveFinalizer(cluster, Finalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return requeue.Error(err)
		}
		return requeue.Never()
	}

	if err := r.Provider.DeleteCluster(name); err != nil {
		return requeue.Error(err)
	}
	return requeue.Progressing()
}

func (r *ClusterReconciler) handleCreateOrUpdate(ctx context.Context, cluster *v1alpha1.Cluster) (ctrl.Result, error) {
	requeue := smartrequeue.FromContext(ctx)
	cluster.Status.Phase = v1alpha1.Progressing

	if controllerutil.AddFinalizer(cluster, Finalizer) {
		if err := r.Update(ctx, cluster); err != nil {
			return requeue.Error(err)
		}
	}

	if err := r.assignSubnet(ctx, cluster); err != nil {
		return requeue.Error(err)
	}

	name := kindName(cluster)

	exists, err := r.Provider.ClusterExists(name)
	if err != nil {
		return requeue.Error(err)
	}

	if !exists {
		if err := r.Provider.CreateCluster(name); err != nil {
			return requeue.Error(err)
		}

		return requeue.Progressing()
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:   string(v1alpha1.KindReady),
		Status: metav1.ConditionTrue,
		Reason: "ClusterExists",
	})

	kubeconfig, err := r.Provider.KubeConfig(name)
	if err != nil {
		return requeue.Error(err)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return requeue.Error(err)
	}

	kindClient, err := client.New(cfg, client.Options{Scheme: r.Scheme})
	if err != nil {
		return requeue.Error(err)
	}

	cNet, err := kind.SubnetFromCluster(cluster)
	if err != nil {
		return requeue.Error(err)
	}

	if err := metallb.Install(ctx, kindClient); err != nil {
		return requeue.Error(err)
	}

	metallbReady, err := metallb.IsReady(ctx, kindClient)
	if err != nil {
		return requeue.Error(err)
	}
	if !metallbReady {
		return requeue.Progressing()
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:   string(v1alpha1.MetalLBReady),
		Status: metav1.ConditionTrue,
		Reason: "AllPodsReady",
	})

	if err := metallb.ConfigureSubnet(ctx, kindClient, *cNet); err != nil {
		return requeue.Error(err)
	}

	cluster.Status.Phase = v1alpha1.Ready
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:   string(v1alpha1.Ready),
		Status: metav1.ConditionTrue,
		Reason: "ClusterAndMetalLBReady",
	})
	return requeue.Stable()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Cluster{}).
		Named("cluster").
		Complete(r)
}

func (r *ClusterReconciler) assignSubnet(ctx context.Context, cluster *v1alpha1.Cluster) error {
	_, ok := cluster.Annotations[kind.AnnotationAssignedSubnet]
	if ok {
		return nil
	}

	availableNet, err := kind.NextAvailableLBNetwork(ctx, r.Client)
	if err != nil {
		return err
	}

	metav1.SetMetaDataAnnotation(&cluster.ObjectMeta, kind.AnnotationAssignedSubnet, availableNet.String())
	return r.Update(ctx, cluster)
}

func kindName(cluster *v1alpha1.Cluster) string {
	return fmt.Sprintf("%s.%s", namespaceOrDefault(cluster.Namespace), cluster.Name)
}

func namespaceOrDefault(namespace string) string {
	if namespace == "" {
		return metav1.NamespaceDefault
	}
	return namespace
}
