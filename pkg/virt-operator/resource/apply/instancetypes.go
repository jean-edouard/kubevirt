package apply

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/api/instancetype/v1beta1"
	"kubevirt.io/client-go/log"
)

func (r *Reconciler) createOrUpdateInstancetypes() error {
	for _, instancetype := range r.targetStrategy.Instancetypes() {
		gvk := instancetype.GroupVersionKind()
		gvr := schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: "virtualmachineclusterinstancetypes",
		}
		object := instancetype.DeepCopy()
		if err := r.createOrUpdateObject(r.clientset.DynamicClient().Resource(gvr), object, object); err != nil {
			return err
		}
	}

	return nil
}

func specsAreEqual(object1, object2 runtime.Object) bool {
	switch object1.(type) {
	case *v1beta1.VirtualMachineClusterInstancetype:
		instancetype1 := object1.(*v1beta1.VirtualMachineClusterInstancetype)
		instancetype2, success := object2.(*v1beta1.VirtualMachineClusterInstancetype)
		if !success {
			return false
		}
		return equality.Semantic.DeepEqual(instancetype1.Spec, instancetype2.Spec)
	case *v1beta1.VirtualMachinePreference:
		preference1 := object1.(*v1beta1.VirtualMachinePreference)
		preference2, success := object2.(*v1beta1.VirtualMachinePreference)
		if !success {
			return false
		}
		return equality.Semantic.DeepEqual(preference1.Spec, preference2.Spec)
	}
	return false
}

func (r *Reconciler) createOrUpdateObject(client dynamic.NamespaceableResourceInterface, object metav1.Object, rObj runtime.Object) error {
	accessor, err := meta.Accessor(object)
	if err != nil {
		return err
	}
	objectMetaAcessor := meta.AsPartialObjectMetadata(object)
	foundObj, err := client.Get(context.Background(), accessor.GetName(), metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	imageTag, imageRegistry, id := getTargetVersionRegistryID(r.kv)
	injectOperatorMetadata(r.kv, &objectMetaAcessor.ObjectMeta, imageTag, imageRegistry, id, true)

	if errors.IsNotFound(err) {
		if _, err := client.Create(context.Background(), object.(*unstructured.Unstructured), metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to create object %s: %s", accessor.GetName(), err.Error())
		}
		log.Log.V(2).Infof("object %s created", accessor.GetName())
		return nil
	}

	if equality.Semantic.DeepEqual(foundObj.GetAnnotations(), accessor.GetAnnotations()) &&
		equality.Semantic.DeepEqual(foundObj.GetLabels(), accessor.GetLabels()) &&
		specsAreEqual(foundObj.DeepCopyObject(), rObj) {
		log.Log.V(4).Infof("object %s is up-to-date", accessor.GetName())
		return nil
	}

	accessor.SetResourceVersion(foundObj.GetResourceVersion())
	if _, err := client.Update(context.Background(), object.(*unstructured.Unstructured), metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("unable to update object %s: %s", accessor.GetName(), err.Error())
	}
	log.Log.V(2).Infof("object %s updated", accessor.GetName())

	return nil
}

func (r *Reconciler) deleteInstancetypes() error {
	ls := labels.Set{
		v1.AppComponentLabel: v1.AppComponent,
		v1.ManagedByLabel:    v1.ManagedByLabelOperatorValue,
	}

	if err := r.clientset.VirtualMachineClusterInstancetype().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: ls.String(),
	}); err != nil {
		return fmt.Errorf("unable to delete preferences: %v", err)
	}

	return nil
}

func (r *Reconciler) createOrUpdatePreferences() error {
	for _, preference := range r.targetStrategy.Preferences() {
		gvk := preference.GroupVersionKind()
		gvr := schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: "virtualmachineclusterinstancetypes",
		}
		object := preference.DeepCopy()
		if err := r.createOrUpdateObject(r.clientset.DynamicClient().Resource(gvr), object, object); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reconciler) deletePreferences() error {
	ls := labels.Set{
		v1.AppComponentLabel: v1.AppComponent,
		v1.ManagedByLabel:    v1.ManagedByLabelOperatorValue,
	}

	if err := r.clientset.VirtualMachineClusterPreference().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: ls.String(),
	}); err != nil {
		return fmt.Errorf("unable to delete preferences: %v", err)
	}

	return nil
}
