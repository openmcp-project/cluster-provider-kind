package metallb

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

//go:generate curl -L -o metallb-native.yaml https://raw.githubusercontent.com/metallb/metallb/v0.14.9/config/manifests/metallb-native.yaml

var (
	//go:embed metallb-native.yaml
	metallbYAML []byte
)

const (
	namespace = "metallb-system"

	resourceBaseYAML      = "metallb-native.yaml"
	resourceKustomization = "kustomization.yaml"
)

// Install installs the MetalLB components in the cluster.
func Install(ctx context.Context, c client.Client) error {
	r, err := build()
	if err != nil {
		return err
	}

	objs, err := toUnstructured(r)
	if err != nil {
		return err
	}

	return createObjects(ctx, c, objs)
}

// ConfigureSubnet configures the MetalLB subnet for the cluster.
func ConfigureSubnet(ctx context.Context, c client.Client, subnet net.IPNet) error {
	return errors.Join(
		configureIPAddressPool(ctx, c, subnet),
		configureL2Advertisement(ctx, c),
	)
}

func configureIPAddressPool(ctx context.Context, c client.Client, subnet net.IPNet) error {
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "metallb.io",
		Version: "v1beta1",
		Kind:    "IPAddressPool",
	})
	pool.SetName("kind")
	pool.SetNamespace(namespace)

	_, err := controllerutil.CreateOrUpdate(ctx, c, pool, func() error {
		pool.Object["spec"] = map[string]any{
			"addresses": []string{
				subnet.String(),
			},
			"avoidBuggyIPs": true,
		}
		return nil
	})
	return err
}

func configureL2Advertisement(ctx context.Context, c client.Client) error {
	l2a := &unstructured.Unstructured{}
	l2a.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "metallb.io",
		Version: "v1beta1",
		Kind:    "L2Advertisement",
	})
	l2a.SetName("kind")
	l2a.SetNamespace(namespace)

	_, err := controllerutil.CreateOrUpdate(ctx, c, l2a, func() error {
		// nothing to update
		return nil
	})
	return err
}

func toUnstructured(r resmap.ResMap) ([]*unstructured.Unstructured, error) {
	objs := []*unstructured.Unstructured{}

	for _, res := range r.Resources() {
		yamlBytes, err := res.AsYAML()
		if err != nil {
			return nil, err
		}

		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(yamlBytes, u); err != nil {
			return nil, err
		}

		objs = append(objs, u)
	}

	return objs, nil
}

func createObjects(ctx context.Context, c client.Client, objs []*unstructured.Unstructured) error {
	for _, obj := range objs {
		if err := c.Create(ctx, obj); client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create object: %v", err)
		}
	}

	return nil
}

func build() (resmap.ResMap, error) {
	fs := filesys.MakeFsInMemory()

	err := errors.Join(
		addBaseYAMLToFS(fs),
		addKustomizationToFS(fs, nil),
	)
	if err != nil {
		return nil, err
	}

	// Recover if Kustomize panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("kustomize panic: %v", r)
		}
	}()

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	return k.Run(fs, ".")
}

func addBaseYAMLToFS(fs filesys.FileSystem) error {
	return fs.WriteFile(resourceBaseYAML, metallbYAML)
}

func addKustomizationToFS(fs filesys.FileSystem, images []types.Image) error {
	k := getKustomization(images)
	jsonBytes, err := json.Marshal(k)
	if err != nil {
		return err
	}

	return fs.WriteFile(resourceKustomization, jsonBytes)
}

func getKustomization(images []types.Image) *types.Kustomization {
	return &types.Kustomization{
		TypeMeta: types.TypeMeta{
			Kind:       types.KustomizationKind,
			APIVersion: types.KustomizationVersion,
		},
		Namespace: namespace,
		Resources: []string{
			resourceBaseYAML,
		},
		Images: images,
		Labels: []types.Label{
			{
				Pairs: map[string]string{
					"app.kubernetes.io/managed-by": "cluster-provider-kind",
				},
			},
		},
	}
}
