package metallb

import (
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsReady checks if the MetalLB components are ready.
func IsReady(ctx context.Context, c client.Client) (bool, error) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.MatchingLabels{"app": "metallb"}); err != nil {
		return false, err
	}

	for _, pod := range pods.Items {
		if !isPodReady(&pod) {
			return false, nil
		}
	}
	return true, nil
}

func isPodReady(pod *corev1.Pod) bool {
	return slices.ContainsFunc(pod.Status.Conditions, func(c corev1.PodCondition) bool {
		return c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue
	})
}
