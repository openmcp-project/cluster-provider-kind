package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestIdentifyFinalizers(t *testing.T) {
	tests := []struct {
		name                      string
		inputFinalizers           []string
		expectedForeignFinalizers []string
		expectedOwnFinalizer      bool
	}{
		{
			name:                      "no finalizers on the object at all",
			inputFinalizers:           []string{},
			expectedForeignFinalizers: []string{},
			expectedOwnFinalizer:      false,
		},
		{
			name:                      "only the own finalizer on the object",
			inputFinalizers:           []string{Finalizer},
			expectedForeignFinalizers: []string{},
			expectedOwnFinalizer:      true,
		},
		{
			name:                      "only other finalizers on the object",
			inputFinalizers:           []string{"other/finalizer1", "other/finalizer2"},
			expectedForeignFinalizers: []string{"other/finalizer1", "other/finalizer2"},
			expectedOwnFinalizer:      false,
		},
		{
			name:                      "both own and other finalizers on the object",
			inputFinalizers:           []string{"other/finalizer1", Finalizer, "other/finalizer2"},
			expectedForeignFinalizers: []string{"other/finalizer1", "other/finalizer2"},
			expectedOwnFinalizer:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &corev1.Namespace{}

			for _, finalizer := range tt.inputFinalizers {
				controllerutil.AddFinalizer(obj, finalizer)
			}

			foreignFinalizers, ownFinalizer := identifyFinalizers(obj)

			if !reflect.DeepEqual(foreignFinalizers, tt.expectedForeignFinalizers) {
				t.Errorf("identifyFinalizers() foreignFinalizers = %v, want %v", foreignFinalizers, tt.expectedForeignFinalizers)
			}
			if ownFinalizer != tt.expectedOwnFinalizer {
				t.Errorf("identifyFinalizers() ownFinalizer = %v, want %v", ownFinalizer, tt.expectedOwnFinalizer)
			}
		})
	}
}
