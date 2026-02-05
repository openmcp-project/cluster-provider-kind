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
	"os"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"

	"github.com/openmcp-project/cluster-provider-kind/api/v1alpha1"
	"github.com/openmcp-project/cluster-provider-kind/pkg/kind"
	"github.com/openmcp-project/cluster-provider-kind/pkg/metallb"
	"github.com/openmcp-project/cluster-provider-kind/pkg/smartrequeue"
)

var (
	// Finalizer is the finalizer for Cluster
	Finalizer = clustersv1alpha1.GroupVersion.Group + "/finalizer"

	// AnnotationName can be used to override the name of the kind cluster.
	AnnotationName = v1alpha1.GroupVersion.Group + "/name"
)

const (
	profileKind = "kind"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	RequeueStore *smartrequeue.Store
	Provider     kind.Provider
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile")
	defer log.Info("Done")

	cluster := &clustersv1alpha1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !isClusterProviderResponsible(cluster) {
		return ctrl.Result{}, fmt.Errorf("profile '%s' is not supported by kind controller", cluster.Spec.Profile)
	}

	prevStatus := cluster.DeepCopy().Status
	ctx = smartrequeue.NewContext(ctx, r.RequeueStore.For(cluster))

	var result ctrl.Result
	var err error

	if cluster.DeletionTimestamp.IsZero() {
		result, err = r.handleCreateOrUpdate(ctx, cluster)
	} else {
		result, err = r.handleDelete(ctx, cluster)
	}

	if err != nil {
		return result, err
	}

	if !equality.Semantic.DeepEqual(prevStatus, cluster.Status) {
		return result, r.Status().Update(ctx, cluster)
	}

	return result, nil
}

func (r *ClusterReconciler) handleDelete(ctx context.Context, cluster *clustersv1alpha1.Cluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	requeue := smartrequeue.FromContext(ctx)
	cluster.Status.Phase = commonapi.StatusPhaseTerminating

	// check if there are any foreign finalizers on the Cluster resource
	foreignFinalizers, found := identifyFinalizers(cluster)
	if !found {
		// Nothing to do
		return ctrl.Result{}, nil
	}
	if len(foreignFinalizers) > 0 {
		log.Info("Postponing cluster deletion until foreign finalizers are removed", "foreignFinalizers", foreignFinalizers)
		return requeue.Progressing()
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

//nolint:gocyclo
func (r *ClusterReconciler) handleCreateOrUpdate(ctx context.Context, cluster *clustersv1alpha1.Cluster) (ctrl.Result, error) {
	requeue := smartrequeue.FromContext(ctx)

	if controllerutil.AddFinalizer(cluster, Finalizer) {
		if err := r.Update(ctx, cluster); err != nil {
			return requeue.Error(err)
		}

		// Return to prevent conflict on subsequent update.
		// (The update triggers another reconciliation anyway, skipping this block.)
		return requeue.Never()
	}

	cluster.Status.Phase = commonapi.StatusPhaseProgressing

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
		Type:   string("KindReady"),
		Status: metav1.ConditionTrue,
		Reason: "ClusterExists",
	})
	// TODO: Add kind cluster name to status

	kubeconfig, err := r.Provider.KubeConfig(name, runsOnLocalHost())
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
		Type:   "MetalLBReady",
		Status: metav1.ConditionTrue,
		Reason: "AllPodsReady",
	})

	if err := metallb.ConfigureSubnet(ctx, kindClient, *cNet); err != nil {
		return requeue.Error(err)
	}

	cluster.Status.Phase = commonapi.StatusPhaseReady
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:   string(commonapi.StatusPhaseReady),
		Status: metav1.ConditionTrue,
		Reason: "ClusterAndMetalLBReady",
	})
	return requeue.Stable()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.Cluster{}).
		Named("cluster").
		Complete(r)
}

func (r *ClusterReconciler) assignSubnet(ctx context.Context, cluster *clustersv1alpha1.Cluster) error {
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

func kindName(cluster *clustersv1alpha1.Cluster) string {
	if name, ok := cluster.Annotations[AnnotationName]; ok {
		return name
	}
	return fmt.Sprintf("%s.%s", cluster.Name, string(cluster.UID)[:8])
}

func isClusterProviderResponsible(cluster *clustersv1alpha1.Cluster) bool {
	return cluster.Spec.Profile == profileKind
}

// runsOnLocalHost returns true if the KIND_ON_LOCAL_HOST environment variable is set to "true".
func runsOnLocalHost() bool {
	return os.Getenv("KIND_ON_LOCAL_HOST") == "true"
}

// identifyFinalizers checks two things for the given object:
// 1. If the 'clusters.openmcp.cloud/finalizer' finalizer is present (second return value).
// 2. Which other finalizers are present (first return value).
func identifyFinalizers(obj client.Object) ([]string, bool) {
	foreignFinalizers := make([]string, 0, len(obj.GetFinalizers()))
	found := false
	for _, fin := range obj.GetFinalizers() {
		if fin != Finalizer {
			foreignFinalizers = append(foreignFinalizers, fin)
		} else {
			found = true
		}
	}
	return foreignFinalizers, found
}
