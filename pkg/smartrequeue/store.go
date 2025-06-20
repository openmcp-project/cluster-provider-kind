package smartrequeue

import (
	"reflect"
	"sync"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewStore creates a new Store with the specified minimum and maximum intervals
// and a multiplier for the exponential backoff logic.
func NewStore(minInterval, maxInterval time.Duration, multiplier float32) *Store {
	return &Store{
		minInterval: minInterval,
		maxInterval: maxInterval,
		multiplier:  multiplier,
		objects:     map[key]*Entry{},
	}
}

// Store is used to manage requeue entries for different objects.
// It holds a map of entries indexed by a key that uniquely identifies the object.
type Store struct {
	minInterval time.Duration
	maxInterval time.Duration
	multiplier  float32
	objects     map[key]*Entry
	objectsLock sync.Mutex
}

func (s *Store) For(obj client.Object) *Entry {
	s.objectsLock.Lock()
	defer s.objectsLock.Unlock()

	objKey := keyFromObject(obj)
	entry, ok := s.objects[objKey]

	if !ok {
		entry = newEntry(s)
		s.objects[objKey] = entry
	}

	return entry
}

func (s *Store) deleteEntry(toDelete *Entry) {
	s.objectsLock.Lock()
	defer s.objectsLock.Unlock()

	for i, entry := range s.objects {
		if entry == toDelete {
			delete(s.objects, i)
			break
		}
	}
}

func keyFromObject(obj client.Object) key {
	return key{
		Kind:      reflect.TypeOf(obj).Elem().Name(),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
}

type key struct {
	Kind      string
	Name      string
	Namespace string
}

func newEntry(s *Store) *Entry {
	return &Entry{
		store:        s,
		nextDuration: s.minInterval,
	}
}

// Entry is used to manage the requeue logic for a specific object.
// It holds the next duration to requeue and the store it belongs to.
type Entry struct {
	store        *Store
	nextDuration time.Duration
}

// Error resets the duration to the minInterval and returns an empty Result and the error
// so that the controller-runtime can handle the exponential backoff for errors.
func (e *Entry) Error(err error) (ctrl.Result, error) {
	e.nextDuration = e.store.minInterval
	e.setNext()
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
