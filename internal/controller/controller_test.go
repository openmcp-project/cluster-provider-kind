package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Helper Functions Test", func() {

	Context("identifyFinalizers", func() {

		It("should work correctly with no finalizers on the object at all", func() {
			obj := &corev1.Namespace{}
			ff, own := identifyFinalizers(obj)
			Expect(ff).To(BeEmpty())
			Expect(own).To(BeFalse())
		})

		It("should work correctly with only the own finalizer on the object", func() {
			obj := &corev1.Namespace{}
			controllerutil.AddFinalizer(obj, Finalizer)
			ff, own := identifyFinalizers(obj)
			Expect(ff).To(BeEmpty())
			Expect(own).To(BeTrue())
		})

		It("should work correctly with only other finalizers on the object", func() {
			obj := &corev1.Namespace{}
			controllerutil.AddFinalizer(obj, "other/finalizer1")
			controllerutil.AddFinalizer(obj, "other/finalizer2")
			ff, own := identifyFinalizers(obj)
			Expect(ff).To(ConsistOf("other/finalizer1", "other/finalizer2"))
			Expect(own).To(BeFalse())
		})

		It("should work correctly with both own and other finalizers on the object", func() {
			obj := &corev1.Namespace{}
			controllerutil.AddFinalizer(obj, "other/finalizer1")
			controllerutil.AddFinalizer(obj, Finalizer)
			controllerutil.AddFinalizer(obj, "other/finalizer2")
			ff, own := identifyFinalizers(obj)
			Expect(ff).To(ConsistOf("other/finalizer1", "other/finalizer2"))
			Expect(own).To(BeTrue())
		})

	})

})
