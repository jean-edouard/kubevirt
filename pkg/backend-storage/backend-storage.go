package backendstorage

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"kubevirt.io/client-go/log"

	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
)

const (
	PVCPrefix   = "persistent-state-for-"
	PVCSize     = "10Mi"
	IsMigration = true
)

func HasPersistentTPMDevice(vmi *corev1.VirtualMachineInstance) bool {
	if vmi.Spec.Domain.Devices.TPM != nil &&
		vmi.Spec.Domain.Devices.TPM.Persistent != nil &&
		*vmi.Spec.Domain.Devices.TPM.Persistent {
		return true
	}

	return false
}

func isBackendStorageNeeded(vmi *corev1.VirtualMachineInstance) bool {
	return HasPersistentTPMDevice(vmi)
}

func CreateIfNeeded(vmi *corev1.VirtualMachineInstance, clusterConfig *virtconfig.ClusterConfig, client kubecli.KubevirtClient, isMigration bool) (string, error) {
	if !isBackendStorageNeeded(vmi) {
		return "", nil
	}

	// Look for an existing backend storage PVC for the VM
	pvcList, err := client.CoreV1().PersistentVolumeClaims(vmi.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "kubevirt.io/vm=" + vmi.Name,
	})
	if err != nil {
		if !errors.IsNotFound(err) {
			return "", err
		}
	} else if len(pvcList.Items) > 1 {
		return "", fmt.Errorf("found too many backend storage PVCs for %s", vmi.Name)
	} else if len(pvcList.Items) == 1 {
		// Exactly one existing backend storage PVC was found for the VM(I), use it, unless this is a migration target.
		if !isMigration {
			return pvcList.Items[0].Name, nil
		}
	}

	modeFile := v1.PersistentVolumeFilesystem
	storageClass := clusterConfig.GetBackendStorageClass()
	if storageClass == "" {
		return "", fmt.Errorf("backend VM storage requires a backend storage class defined in the custom resource")
	}
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: PVCPrefix + vmi.Name + "-",
			Labels:       map[string]string{"kubevirt.io/vm": vmi.Name},
			// TODO? If the VM vmi.Name exists, mark is as owner for auto PVC deletion on VM deletion?
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{v1.ResourceStorage: resource.MustParse(PVCSize)},
			},
			StorageClassName: &storageClass,
			VolumeMode:       &modeFile,
		},
	}

	pvc, err = client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Create(context.Background(), pvc, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	return pvc.ObjectMeta.Name, nil
}

func RemoveOldPVCFor(vmi string, pod *v1.Pod, client kubecli.KubevirtClient) {
	if pod == nil {
		return
	}
	toKeep := ""
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			toKeep = volume.PersistentVolumeClaim.ClaimName
			break
		}
	}
	if toKeep == "" {
		return
	}
	pvcList, err := client.CoreV1().PersistentVolumeClaims(pod.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "kubevirt.io/vm=" + vmi,
	})
	if err != nil {
		return
	}
	for _, pvc := range pvcList.Items {
		if pvc.Name != toKeep {
			_ = client.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(context.Background(), pvc.Name, metav1.DeleteOptions{})
		}
	}
}

func RemovePVCFor(pod *v1.Pod, client kubecli.KubevirtClient) {
	if pod == nil {
		return
	}
	toRemove := ""
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			toRemove = volume.PersistentVolumeClaim.ClaimName
			break
		}
	}
	if toRemove == "" {
		return
	}
	err := client.CoreV1().PersistentVolumeClaims(pod.Namespace).Delete(context.Background(), toRemove, metav1.DeleteOptions{})
	if err != nil {
		log.Log.Warningf("Failed to remove backend storage PVC %s", toRemove)
	}
}
