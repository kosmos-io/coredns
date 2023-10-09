package kubernetes

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

const corednsFinalizer = "clusterlink.io/coredns"

type FinalizerHelper interface {
	ContainsFinalizer(obj *unstructured.Unstructured, finalizer string) bool
	RemoveFinalizer(obj *unstructured.Unstructured, finalizer string) bool
	AddFinalizer(obj *unstructured.Unstructured, finalizer string) bool
}

func ContainsFinalizer(obj *unstructured.Unstructured, finalizer string) bool {
	finalizers := obj.GetFinalizers()
	for _, str := range finalizers {
		if str == finalizer {
			return true
		}
	}
	return false
}

func RemoveFinalizer(obj *unstructured.Unstructured, finalizer string) (finalizersUpdated bool) {
	finalizers := obj.GetFinalizers()
	for i := 0; i < len(finalizers); i++ {
		if finalizers[i] == finalizer {
			finalizers = append(finalizers[:i], finalizers[i+1:]...)
			i--
			finalizersUpdated = true
		}
	}
	obj.SetFinalizers(finalizers)
	return
}

func AddFinalizer(obj *unstructured.Unstructured, finalizer string) (finalizersUpdated bool) {
	finalizers := obj.GetFinalizers()
	for _, e := range finalizers {
		if e == finalizer {
			return false
		}
	}
	obj.SetFinalizers(append(finalizers, finalizer))
	return true
}
