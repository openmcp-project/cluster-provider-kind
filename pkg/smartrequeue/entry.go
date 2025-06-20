package smartrequeue

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

// Entry is used to manage the requeue logic for a specific object.
// It holds the next duration to requeue and the store it belongs to.
type Entry struct {
	store        *Store
	nextDuration time.Duration
}

func newEntry(s *Store) *Entry {
	return &Entry{
		store:        s,
		nextDuration: s.minInterval,
	}
}

// Error resets the duration to the minInterval and returns an empty Result and the error
// so that the controller-runtime can handle the exponential backoff for errors.
func (e *Entry) Error(err error) (ctrl.Result, error) {
	e.nextDuration = e.store.minInterval
	return ctrl.Result{}, err
}

// Stable returns a Result and increments the interval for the next iteration.
// Used when the external resource is stable (healthy or unhealthy).
func (e *Entry) Stable() (ctrl.Result, error) {
	defer e.setNext()
	return ctrl.Result{RequeueAfter: e.nextDuration}, nil
}

// Progressing resets the duration to the minInterval and returns a Result with that interval.
// Used when the external resource is still doing something (creating, deleting, updating, etc.)
func (e *Entry) Progressing() (ctrl.Result, error) {
	e.nextDuration = e.store.minInterval
	defer e.setNext()
	return ctrl.Result{RequeueAfter: e.nextDuration}, nil
}

// Never deletes the entry from the store and returns an empty Result.
func (e *Entry) Never() (ctrl.Result, error) {
	e.store.deleteEntry(e)
	return ctrl.Result{}, nil
}

// setNext updates the next requeue duration using exponential backoff.
// It multiplies the current duration by the store's multiplier and ensures
// the result doesn't exceed the configured maximum interval.
func (e *Entry) setNext() {
	newDuration := time.Duration(float32(e.nextDuration) * e.store.multiplier)

	if newDuration > e.store.maxInterval {
		newDuration = e.store.maxInterval
	}

	e.nextDuration = newDuration
}
