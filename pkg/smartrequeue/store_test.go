package smartrequeue

import (
	"testing"
	"time"

	"github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/stretchr/testify/assert"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_Entry(t *testing.T) {
	entry := newEntry(NewStore(time.Second, time.Minute, 2))

	assert.Equal(t, 1*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 2*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 4*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 8*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 16*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 32*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 60*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 60*time.Second, getRequeueAfter(entry.Stable()))

	assert.Equal(t, 1*time.Second, getRequeueAfter(entry.Progressing()))
	assert.Equal(t, 1*time.Second, getRequeueAfter(entry.Progressing()))

	assert.Equal(t, 2*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 4*time.Second, getRequeueAfter(entry.Stable()))

	assert.Equal(t, 0*time.Second, getRequeueAfter(entry.Error(assert.AnError)))
	assert.Equal(t, 2*time.Second, getRequeueAfter(entry.Stable()))
	assert.Equal(t, 4*time.Second, getRequeueAfter(entry.Stable()))
}

func getRequeueAfter(res ctrl.Result, _ error) time.Duration {
	return res.RequeueAfter.Round(time.Second)
}

func TestFor(t *testing.T) {
	tests := []struct {
		name        string
		firstObj    client.Object
		secondObj   client.Object
		expectSame  bool
		description string
	}{
		{
			name: "same object returns same entry",
			firstObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			secondObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			expectSame:  true,
			description: "Expected to get the same entry back",
		},
		{
			name: "different namespace returns different entry",
			firstObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			secondObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test2",
				},
			},
			expectSame:  false,
			description: "Expected to get a different entry back",
		},
		{
			name: "different name returns different entry",
			firstObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			secondObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test2",
					Namespace: "test",
				},
			},
			expectSame:  false,
			description: "Expected to get a different entry back",
		},
		{
			name: "different kind returns different entry",
			firstObj: &v1alpha1.Cluster{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			secondObj: &v1alpha1.AccessRequest{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			expectSame:  false,
			description: "Expected to get a different entry back",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStore(time.Second, time.Minute, 2)
			entry1 := store.For(tt.firstObj)

			assert.NotNil(t, entry1, "Expected entry to be created")
			assert.Equal(t, 1*time.Second, getRequeueAfter(entry1.Stable()))

			entry2 := store.For(tt.secondObj)

			if tt.expectSame {
				assert.Equal(t, entry1, entry2, tt.description)
			} else {
				assert.NotEqual(t, entry1, entry2, tt.description)
			}
		})
	}
}
