package smartrequeue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	ctrl "sigs.k8s.io/controller-runtime"
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
