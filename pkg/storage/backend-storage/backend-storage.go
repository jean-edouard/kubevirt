/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2023 Red Hat, Inc.
 *
 */

package backendstorage

import (
	"context"
	"fmt"
	"sync"

	"kubevirt.io/client-go/log"

	"kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"

	"k8s.io/client-go/tools/cache"

	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"

	"k8s.io/apimachinery/pkg/api/errors"

	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
)

const (
	PVCPrefix = "persistent-state-for-"
	PVCSize   = "10Mi"
)

func PVCForVMI(vmi *corev1.VirtualMachineInstance) string {
	return PVCPrefix + vmi.Name
}

func HasPersistentTPMDevice(vmiSpec *corev1.VirtualMachineInstanceSpec) bool {
	return vmiSpec.Domain.Devices.TPM != nil &&
		vmiSpec.Domain.Devices.TPM.Persistent != nil &&
		*vmiSpec.Domain.Devices.TPM.Persistent
}

func HasPersistentEFI(vmiSpec *corev1.VirtualMachineInstanceSpec) bool {
	return vmiSpec.Domain.Firmware != nil &&
		vmiSpec.Domain.Firmware.Bootloader != nil &&
		vmiSpec.Domain.Firmware.Bootloader.EFI != nil &&
		vmiSpec.Domain.Firmware.Bootloader.EFI.Persistent != nil &&
		*vmiSpec.Domain.Firmware.Bootloader.EFI.Persistent
}

func IsBackendStorageNeededForVMI(vmiSpec *corev1.VirtualMachineInstanceSpec) bool {
	return HasPersistentTPMDevice(vmiSpec) || HasPersistentEFI(vmiSpec)
}

func IsBackendStorageNeededForVM(vm *corev1.VirtualMachine) bool {
	if vm.Spec.Template == nil {
		return false
	}
	return HasPersistentTPMDevice(&vm.Spec.Template.Spec)
}

type BackendStorage struct {
	client          kubecli.KubevirtClient
	clusterConfig   *virtconfig.ClusterConfig
	scStore         cache.Store
	accessModeCache map[string]v1.PersistentVolumeAccessMode
	cacheLock       sync.Mutex
}

func NewBackendStorage(client kubecli.KubevirtClient, clusterConfig *virtconfig.ClusterConfig, scStore cache.Store) *BackendStorage {
	return &BackendStorage{
		client:          client,
		clusterConfig:   clusterConfig,
		scStore:         scStore,
		accessModeCache: map[string]v1.PersistentVolumeAccessMode{},
	}
}

func (bs *BackendStorage) getStorageClass() (string, error) {
	storageClass := bs.clusterConfig.GetVMStateStorageClass()
	if storageClass != "" {
		return storageClass, nil
	}

	for _, obj := range bs.scStore.List() {
		sc := obj.(*storagev1.StorageClass)
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			return sc.Name, nil
		}
	}

	return "", fmt.Errorf("no default storage class found")
}

func (bs *BackendStorage) getMode(storageClass string, mode v1.PersistentVolumeMode) v1.PersistentVolumeAccessMode {
	var storageProfile *v1beta1.StorageProfile
	bs.cacheLock.Lock()
	defer bs.cacheLock.Unlock()
	accessMode, exists := bs.accessModeCache[storageClass]
	if exists {
		return accessMode
	}

	// status.storageClass is unfortunately not a supported field label.
	// If storage profiles are guaranteed to have the same name as their storage class, this could just be a get.
	sps, err := bs.client.CdiClient().CdiV1beta1().StorageProfiles().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		// CDI not present most likely
		log.Log.Reason(err).Info("couldn't list storage profiles, defaulting to RWX")
		return v1.ReadWriteMany
	}
	for _, sp := range sps.Items {
		if sp.Status.StorageClass != nil && *sp.Status.StorageClass == storageClass {
			storageProfile = &sp
			break
		}
	}
	if storageProfile == nil {
		log.Log.Infof("no storage profile found for %s, defaulting to RWX", storageClass)
		return v1.ReadWriteMany
	}

	bs.accessModeCache[storageClass] = v1.ReadWriteMany
	if storageProfile.Status.ClaimPropertySets == nil || len(storageProfile.Status.ClaimPropertySets) == 0 {
		log.Log.Infof("no ClaimPropertySets in storage profile %s, defaulting to RWX", storageProfile.Name)
		return v1.ReadWriteMany
	}

	foundrwo := false
	for _, property := range storageProfile.Status.ClaimPropertySets {
		if property.VolumeMode != nil && *property.VolumeMode == mode {
			if property.AccessModes != nil {
				for _, accessMode = range property.AccessModes {
					if accessMode == v1.ReadWriteMany {
						return v1.ReadWriteMany
					}
					if accessMode == v1.ReadWriteOnce {
						foundrwo = true
					}
				}
			}
		}
	}
	if foundrwo {
		bs.accessModeCache[storageClass] = v1.ReadWriteOnce
		return v1.ReadWriteOnce
	}

	return v1.ReadWriteMany
}

func (bs *BackendStorage) CreateIfNeeded(vmi *corev1.VirtualMachineInstance) error {
	if !IsBackendStorageNeededForVMI(&vmi.Spec) {
		return nil
	}

	_, err := bs.client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Get(context.Background(), PVCForVMI(vmi), metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	storageClass, err := bs.getStorageClass()
	if err != nil {
		return err
	}
	mode := v1.PersistentVolumeFilesystem
	accessMode := bs.getMode(storageClass, mode)
	ownerReferences := vmi.OwnerReferences
	if len(vmi.OwnerReferences) == 0 {
		// If the VMI has no owner, then it did not originate from a VM.
		// In that case, we tie the PVC to the VMI, rendering it quite useless since it won't actually persist.
		// The alternative is to remove this `if` block, allowing the PVC to persist after the VMI is deleted.
		// However, that would pose security and littering concerns.
		ownerReferences = []metav1.OwnerReference{
			*metav1.NewControllerRef(vmi, corev1.VirtualMachineInstanceGroupVersionKind),
		}
	}
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            PVCForVMI(vmi),
			OwnerReferences: ownerReferences,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{accessMode},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{v1.ResourceStorage: resource.MustParse(PVCSize)},
			},
			StorageClassName: &storageClass,
			VolumeMode:       &mode,
		},
	}

	_, err = bs.client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Create(context.Background(), pvc, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}

	return err
}

// IsPVCReady returns true if either:
// - No PVC is needed for the VMI since it doesn't use backend storage
// - The backend storage PVC is bound
// - The backend storage PVC is pending uses a WaitForFirstConsumer storage class
func (bs *BackendStorage) IsPVCReady(vmi *corev1.VirtualMachineInstance) (bool, error) {
	if !IsBackendStorageNeededForVMI(&vmi.Spec) {
		return true, nil
	}

	pvc, err := bs.client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Get(context.Background(), PVCForVMI(vmi), metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	switch pvc.Status.Phase {
	case v1.ClaimBound:
		return true, nil
	case v1.ClaimLost:
		return false, fmt.Errorf("backend storage PVC lost")
	case v1.ClaimPending:
		if pvc.Spec.StorageClassName == nil {
			return false, fmt.Errorf("no storage class name")
		}
		obj, exists, err := bs.scStore.GetByKey(*pvc.Spec.StorageClassName)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("storage class %s not found", *pvc.Spec.StorageClassName)
		}
		sc := obj.(*storagev1.StorageClass)
		if sc.VolumeBindingMode != nil && *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer {
			return true, nil
		}
	}

	return false, nil
}
