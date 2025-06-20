package smartrequeue

import (
	"reflect"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Store is used to manage requeue entries for different objects.
// It holds a map of entries indexed by a key that uniquely identifies the object.
type Store struct {
	minInterval time.Duration
	maxInterval time.Duration
	multiplier  float32
	objects     map[key]*Entry
	objectsLock sync.Mutex
}

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
