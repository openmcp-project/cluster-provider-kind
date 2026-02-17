package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/openmcp-project/cluster-provider-kind/pkg/kind"
)

func TestAccessRequestReconciler_Reconcile(t *testing.T) {
	providerName := "kind"
	SetAccessRequestServiceAccountNamespace("accessrequest")
	SetEnvironment("unit-test")
	SetProviderName(providerName)
	kindClusterRole := "ClusterRole"
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		req     ctrl.Request
		want    ctrl.Result
		ar      clustersv1alpha1.AccessRequest
		wantErr bool
	}{
		{
			name: "test",
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test",
					Name:      "test",
				},
			},
			ar: clustersv1alpha1.AccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"clusters.openmcp.cloud/provider": providerName,
						"clusters.openmcp.cloud/profile":  "test",
					},
				},
				Spec: clustersv1alpha1.AccessRequestSpec{
					ClusterRef: &common.ObjectReference{
						Name: "fakeCluster",
					},
					Token: &clustersv1alpha1.TokenConfig{
						Permissions: []clustersv1alpha1.PermissionsRequest{
							{
								Name:  "test-cluster-role",
								Rules: exampleRules(),
							},
							{
								Name:      "test-role",
								Namespace: "test",
								Rules:     exampleRules(),
							},
						},
						RoleRefs: []common.RoleRef{
							{
								Name: "existing-cluster-role",
								Kind: kindClusterRole,
							},
							{
								Name:      "existing-role",
								Namespace: "existing-test",
								Kind:      kindRole,
							},
						},
					},
				},
				Status: clustersv1alpha1.AccessRequestStatus{
					Status: common.Status{
						ObservedGeneration: 0,
						Phase:              clustersv1alpha1.REQUEST_PENDING,
					},
				},
			},
			want:    ctrl.Result{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = apiextv1.AddToScheme(scheme)
			_ = clustersv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			fakeCluster := clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fakeCluster",
					Annotations: map[string]string{
						"kind.clusters.openmcp.cloud/name": "fakeCluster",
					},
				},
				Spec: clustersv1alpha1.ClusterSpec{
					Profile: "kind",
				},
			}
			fakeClusterClient := fakeClientProvider{
				client:     fake.NewClientBuilder().WithScheme(scheme).Build(),
				restConfig: &rest.Config{},
			}
			r := AccessRequestReconciler{
				ProviderName:    providerName,
				Client:          fake.NewClientBuilder().WithScheme(scheme).WithObjects(&tt.ar, &fakeCluster).Build(),
				Scheme:          scheme,
				ClusterProvider: fakeKindProvider{},
				ClientProvider:  fakeClusterClient,
			}
			got, gotErr := r.Reconcile(ctrl.LoggerInto(context.Background(), zap.New(zap.UseDevMode(true))), tt.req)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Reconcile() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("Reconcile() succeeded unexpectedly")
			}
			assert.Equal(t, time.Duration(0), got.RequeueAfter)

			expectedRules := exampleRules()

			// assert service account exists for this access request
			saList := &corev1.ServiceAccountList{}
			err := fakeClusterClient.client.List(context.TODO(), saList)
			assert.NoError(t, err)
			assert.Len(t, saList.Items, 1)
			sa := saList.Items[0]
			assert.Equal(t, accessRequestServiceAccountNamespace, sa.GetNamespace())

			// assert cluster role exists and has expected rules
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-role",
				},
			}
			err = fakeClusterClient.client.Get(context.TODO(), client.ObjectKeyFromObject(clusterRole), clusterRole)
			assert.NoError(t, err)
			assert.Len(t, clusterRole.Rules, len(expectedRules))
			assert.ElementsMatch(t, expectedRules, clusterRole.Rules)

			// assert role exists and has expected rules
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-role",
					Namespace: "test",
				},
			}
			err = fakeClusterClient.client.Get(context.TODO(), client.ObjectKeyFromObject(role), role)
			assert.NoError(t, err)
			assert.Len(t, role.Rules, len(expectedRules))
			assert.ElementsMatch(t, expectedRules, role.Rules)

			// assert cluster role binding exists
			crbList := &rbacv1.ClusterRoleBindingList{}
			err = fakeClusterClient.client.List(context.TODO(), crbList)
			assert.NoError(t, err)
			// expected: one for the new and one for the 'existing' cluster role
			assert.Len(t, crbList.Items, 2)
			// assert reference to service account
			expectedRoleRefs := []string{"test-cluster-role", "existing-cluster-role"}
			for _, crb := range crbList.Items {
				assert.Contains(t, expectedRoleRefs, crb.RoleRef.Name)
				for _, sub := range crb.Subjects {
					assert.Equal(t, sa.Name, sub.Name)
					assert.Equal(t, sa.Namespace, sub.Namespace)
					assert.Equal(t, "ServiceAccount", sub.Kind)
				}
			}

			// assert role binding exists
			rbList := &rbacv1.RoleBindingList{}
			err = fakeClusterClient.client.List(context.TODO(), rbList)
			assert.NoError(t, err)
			// expected: one for the new and one for the 'existing' cluster role
			assert.Len(t, rbList.Items, 2)
			// assert reference to service account
			expectedRoleRefs = []string{"test-role", "existing-role"}
			for _, rb := range rbList.Items {
				assert.Contains(t, expectedRoleRefs, rb.RoleRef.Name)
				for _, sub := range rb.Subjects {
					assert.Equal(t, sa.Name, sub.Name)
					assert.Equal(t, sa.Namespace, sub.Namespace)
					assert.Equal(t, "ServiceAccount", sub.Kind)
				}
			}

			// assert kubeconfig secret exists
			seList := &corev1.SecretList{}
			r.List(context.TODO(), seList)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s.kubeconfig", tt.req.Name),
					Namespace: tt.req.Namespace,
				},
			}
			err = r.Get(context.Background(), client.ObjectKeyFromObject(secret), secret)
			assert.NoError(t, err)
			_, exists := secret.StringData["kubeconfig"]
			assert.True(t, exists)
		})
	}
}

var _ ClientProvider = fakeClientProvider{}

type fakeClientProvider struct {
	client     client.Client
	restConfig *rest.Config
}

// CreateClient implements [ClientProvider].
func (f fakeClientProvider) CreateClient(kubeconfig string) (client.Client, *rest.Config, error) {
	return f.client, f.restConfig, nil
}

var _ kind.Provider = fakeKindProvider{}

type fakeKindProvider struct{}

// ClusterExists implements [kind.Provider].
func (f fakeKindProvider) ClusterExists(name string) (bool, error) {
	panic("unimplemented")
}

// CreateCluster implements [kind.Provider].
func (f fakeKindProvider) CreateCluster(name string) error {
	panic("unimplemented")
}

// DeleteCluster implements [kind.Provider].
func (f fakeKindProvider) DeleteCluster(name string) error {
	panic("unimplemented")
}

// KubeConfig implements [kind.Provider].
func (f fakeKindProvider) KubeConfig(name string, localhost bool) (string, error) {
	return "testkubeconfig", nil
}

func exampleRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "delete"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"services"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"get", "list", "watch", "update", "patch"},
		},
		{
			APIGroups: []string{"apiextensions.k8s.io"},
			Resources: []string{"customresourcedefinitions"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
}
