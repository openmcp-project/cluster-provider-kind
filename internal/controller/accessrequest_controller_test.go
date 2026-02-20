package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	controllerutilserrors "github.com/openmcp-project/controller-utils/pkg/errors"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/openmcp-project/cluster-provider-kind/pkg/kind"
)

const (
	reqName      = "test"
	reqNamespace = "default"
)

func TestAccessRequestReconciler_Reconcile(t *testing.T) {
	providerName := "kind"
	SetAccessRequestServiceAccountNamespace("accessrequest")
	SetEnvironment("unit-test")
	SetProviderName(providerName)
	kindClusterRole := "ClusterRole"
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = clustersv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		req                  ctrl.Request
		clientProvider       ClientProvider
		ar                   *clustersv1alpha1.AccessRequest
		kubeconfigSecret     *corev1.Secret
		wantErr              bool
		wantReason           string
		wantResourceCreation bool
		wantRefresh          bool
	}{
		{
			name: "no oidc processing",
			req:  request(reqName, reqNamespace),
			clientProvider: fakeClientProvider{
				client:     fake.NewClientBuilder().WithScheme(scheme).Build(),
				restConfig: &rest.Config{},
			},
			ar: accessRequest(reqName, reqNamespace,
				clustersv1alpha1.AccessRequestSpec{
					OIDC: &clustersv1alpha1.OIDCConfig{},
					ClusterRef: &common.ObjectReference{
						Name: "fakeCluster",
					},
				},
				clustersv1alpha1.AccessRequestStatus{
					Status: common.Status{
						Phase: clustersv1alpha1.REQUEST_PENDING,
					},
				}),
			wantErr:              true,
			wantReason:           reasonOIDCRequest,
			wantResourceCreation: false,
			wantRefresh:          false,
		},
		{
			name: "client provider error",
			req:  request(reqName, reqNamespace),
			ar: accessRequest(reqName, reqNamespace,
				clustersv1alpha1.AccessRequestSpec{
					ClusterRef: &common.ObjectReference{
						Name: "fakeCluster",
					},
					Token: &clustersv1alpha1.TokenConfig{},
				},
				clustersv1alpha1.AccessRequestStatus{
					Status: common.Status{
						Phase: clustersv1alpha1.REQUEST_PENDING,
					},
				}),
			clientProvider:       fakeClientProvider{},
			wantErr:              true,
			wantReason:           reasonKindClusterInteractionError,
			wantResourceCreation: false,
			wantRefresh:          false,
		},
		{
			name: "create and delete success",
			req:  request(reqName, reqNamespace),
			clientProvider: fakeClientProvider{
				client:     fake.NewClientBuilder().WithScheme(scheme).Build(),
				restConfig: &rest.Config{},
			},
			ar: accessRequest(reqName, reqNamespace,
				clustersv1alpha1.AccessRequestSpec{
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
				clustersv1alpha1.AccessRequestStatus{
					Status: common.Status{
						Phase: clustersv1alpha1.REQUEST_PENDING,
					},
				}),
			wantErr:              false,
			wantResourceCreation: true,
			wantRefresh:          false,
		},
		{
			name: "refresh expired token",
			req:  request(reqName, reqNamespace),
			clientProvider: fakeClientProvider{
				client:     fake.NewClientBuilder().WithScheme(scheme).Build(),
				restConfig: &rest.Config{},
			},
			ar: accessRequest(reqName, reqNamespace,
				clustersv1alpha1.AccessRequestSpec{
					ClusterRef: &common.ObjectReference{
						Name: "fakeCluster",
					},
					Token: &clustersv1alpha1.TokenConfig{},
				},
				clustersv1alpha1.AccessRequestStatus{
					Status: common.Status{
						Phase: clustersv1alpha1.AccessRequestGranted,
					},
					SecretRef: &common.LocalObjectReference{
						Name: "test.kubeconfig",
					},
				}),
			kubeconfigSecret: secret(types.NamespacedName{
				Name:      "test.kubeconfig",
				Namespace: reqNamespace,
			},
				time.Now().Add(-2*time.Hour),
				time.Now().Add(-1*time.Hour)),
			wantErr:              false,
			wantResourceCreation: false,
			wantRefresh:          true,
		},
		{
			name: "skip refresh of non-expired token",
			req:  request(reqName, reqNamespace),
			clientProvider: fakeClientProvider{
				client:     fake.NewClientBuilder().WithScheme(scheme).Build(),
				restConfig: &rest.Config{},
			},
			ar: accessRequest(reqName, reqNamespace,
				clustersv1alpha1.AccessRequestSpec{
					ClusterRef: &common.ObjectReference{
						Name: "fakeCluster",
					},
					Token: &clustersv1alpha1.TokenConfig{},
				},
				clustersv1alpha1.AccessRequestStatus{
					Status: common.Status{
						Phase: clustersv1alpha1.AccessRequestGranted,
					},
					SecretRef: &common.LocalObjectReference{
						Name: "test.kubeconfig",
					},
				}),
			kubeconfigSecret: secret(types.NamespacedName{
				Name:      "test.kubeconfig",
				Namespace: reqNamespace,
			},
				time.Now().Add(-2*time.Hour),
				time.Now().Add(24*time.Hour)),
			wantErr:              false,
			wantResourceCreation: false,
			wantRefresh:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AccessRequestReconciler{
				ProviderName: providerName,
				Client: fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(buildFakeObject(tt.ar, tt.kubeconfigSecret)...).
					WithStatusSubresource(&clustersv1alpha1.AccessRequest{}).
					Build(),
				Scheme:          scheme,
				ClusterProvider: fakeKindProvider{},
				ClientProvider:  tt.clientProvider,
			}
			ctx := ctrl.LoggerInto(context.Background(), zap.New(zap.UseDevMode(true)))

			// ### RECONCILE ###

			got, gotErr := r.Reconcile(ctx, tt.req)

			// ### ASSERT ERROR ###
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Reconcile() failed: %v", gotErr)
				}
				errWithReason, ok := gotErr.(*controllerutilserrors.ErrorWithReason)
				assert.True(t, ok)
				assert.Equal(t, tt.wantReason, errWithReason.Reason())
				return
			}
			if tt.wantErr {
				t.Fatal("Reconcile() succeeded unexpectedly")
			}

			// ### ASSERT SUCCESS ###

			// always requeue token based requests
			if tt.ar.Spec.Token != nil {
				assert.True(t, got.RequeueAfter > 0)
			}

			if !tt.wantResourceCreation {
				return
			}

			// ### ASSERT RESOURCE CREATION ###

			expectedRules := exampleRules()

			// assert service account exists for this access request
			saList := &corev1.ServiceAccountList{}
			requestedClusterClient, _, _ := tt.clientProvider.CreateClient("")
			err := requestedClusterClient.List(ctx, saList)
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
			err = requestedClusterClient.Get(ctx, client.ObjectKeyFromObject(clusterRole), clusterRole)
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
			err = requestedClusterClient.Get(ctx, client.ObjectKeyFromObject(role), role)
			assert.NoError(t, err)
			assert.Len(t, role.Rules, len(expectedRules))
			assert.ElementsMatch(t, expectedRules, role.Rules)

			// assert cluster role binding exists
			crbList := &rbacv1.ClusterRoleBindingList{}
			err = requestedClusterClient.List(ctx, crbList)
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
			err = requestedClusterClient.List(ctx, rbList)
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

			// assert kubeconfig secret
			seList := &corev1.SecretList{}
			r.List(ctx, seList)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s.kubeconfig", tt.req.Name),
					Namespace: tt.req.Namespace,
				},
			}
			err = r.Get(ctx, client.ObjectKeyFromObject(secret), secret)
			assert.NoError(t, err)
			_, exists := secret.StringData["kubeconfig"]
			assert.True(t, exists)
			ownerSet := secret.OwnerReferences[0].Controller
			assert.True(t, *ownerSet)
			if tt.kubeconfigSecret != nil {
				assert.NotEqual(t, tt.kubeconfigSecret, secret)
			}

			// assert AR status
			ar := &clustersv1alpha1.AccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.req.Name,
					Namespace: tt.req.Namespace,
				},
			}
			assert.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(ar), ar))
			assert.Equal(t, clustersv1alpha1.REQUEST_GRANTED, ar.Status.Phase)
			assert.Equal(t, secret.Name, ar.Status.SecretRef.Name)

			// delete AR
			obj := ar.DeepCopy()
			obj.SetGroupVersionKind(clustersv1alpha1.GroupVersion.WithKind("AccessRequest"))
			err = r.Delete(ctx, obj)
			assert.NoError(t, err)

			// ### RECONCILE DELETE ###

			got, err = r.Reconcile(ctx, tt.req)
			assert.NoError(t, err)

			// ### ASSERT CLEANUP ###

			// service account has been removed
			err = requestedClusterClient.List(ctx, saList)
			assert.NoError(t, err)
			assert.Len(t, saList.Items, 0)

			// roles have been removed
			crList := &rbacv1.ClusterRoleList{}
			err = requestedClusterClient.List(ctx, crList)
			assert.NoError(t, err)
			assert.Len(t, crList.Items, 0)

			rList := &rbacv1.RoleList{}
			err = requestedClusterClient.List(ctx, rList)
			assert.NoError(t, err)
			assert.Len(t, rList.Items, 0)

			// bindings have been removed
			err = requestedClusterClient.List(ctx, crbList)
			assert.NoError(t, err)
			assert.Len(t, crbList.Items, 0)

			err = requestedClusterClient.List(ctx, rbList)
			assert.NoError(t, err)
			assert.Len(t, rbList.Items, 0)

			// secret will get garbage collected via owner reference

			// AR has been removed
			err = r.Get(ctx, client.ObjectKeyFromObject(ar), ar)
			assert.True(t, apierrors.IsNotFound(err))
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
	if f.client == nil || f.restConfig == nil {
		return nil, nil, errors.New("fake client error")
	}
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

func secret(nsn types.NamespacedName, creation, expiration time.Time) *corev1.Secret {
	creConv := strconv.FormatInt(creation.Unix(), 10)
	expConv := strconv.FormatInt(expiration.Unix(), 10)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsn.Name,
			Namespace: nsn.Namespace,
		},
		Data: map[string][]byte{
			clustersv1alpha1.SecretKeyExpirationTimestamp: []byte(expConv),
			clustersv1alpha1.SecretKeyCreationTimestamp:   []byte(creConv),
		},
	}
}

func request(name, namespace string) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func accessRequest(name, namespace string, spec clustersv1alpha1.AccessRequestSpec, status clustersv1alpha1.AccessRequestStatus) *clustersv1alpha1.AccessRequest {
	return &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"clusters.openmcp.cloud/provider": providerName,
				"clusters.openmcp.cloud/profile":  "test",
			},
		},
		Spec:   spec,
		Status: status,
	}
}

func buildFakeObject(objects ...client.Object) []client.Object {
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
	result := []client.Object{&fakeCluster}
	for _, obj := range objects {
		if obj != nil && !reflect.ValueOf(obj).IsNil() {
			result = append(result, obj)
		}
	}
	return result
}
