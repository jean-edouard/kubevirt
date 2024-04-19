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

	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"

	"k8s.io/apimachinery/pkg/api/errors"

	v1 "k8s.io/api/core/v1"
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

func getStorageClass(clusterConfig *virtconfig.ClusterConfig) (string, error) {
	storageClass := clusterConfig.GetVMStateStorageClass()
	if storageClass != "" {
		return storageClass, nil
	}

	// get default storage class for cluster and return that

	return "", fmt.Errorf("no default storage class found")
}

type BackendStorage struct {
	accessModeCache map[string]v1.PersistentVolumeAccessMode
	cacheLock       sync.Mutex
}

func NewBackendStorage() *BackendStorage {
	return &BackendStorage{}
}

func (bs *BackendStorage) getMode(client kubecli.KubevirtClient, storageClass string, mode v1.PersistentVolumeMode) v1.PersistentVolumeAccessMode {
	bs.cacheLock.Lock()
	defer bs.cacheLock.Unlock()
	accessMode, exists := bs.accessModeCache[storageClass]
	if exists {
		return accessMode
	}

	sps, err := client.CdiClient().CdiV1beta1().StorageProfiles().List(context.Background(), metav1.ListOptions{
		FieldSelector: "status.storageClass=" + storageClass,
	})
	if err != nil || len(sps.Items) != 1 {
		return v1.ReadWriteMany
	}

	bs.accessModeCache[storageClass] = v1.ReadWriteMany
	if sps.Items[0].Spec.ClaimPropertySets == nil || len(sps.Items[0].Spec.ClaimPropertySets) == 0 {
		return v1.ReadWriteMany
	}

	foundrwo := false
	for _, property := range sps.Items[0].Spec.ClaimPropertySets {
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

func (bs *BackendStorage) CreateIfNeeded(vmi *corev1.VirtualMachineInstance, clusterConfig *virtconfig.ClusterConfig, client kubecli.KubevirtClient) error {
	if !IsBackendStorageNeededForVMI(&vmi.Spec) {
		return nil
	}

	_, err := client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Get(context.Background(), PVCForVMI(vmi), metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	storageClass, err := getStorageClass(clusterConfig)
	if err != nil {
		return err
	}
	mode := v1.PersistentVolumeFilesystem
	accessMode := bs.getMode(client, storageClass, mode)
	ownerReferences := vmi.OwnerReferences
	if len(vmi.OwnerReferences) == 0 {
		// If the VMI has no owner, then it did not originate from a VM.
		// In that case, we tie the PVC to the VMI, rendering it quite useless since it wont actually persist.
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

	_, err = client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Create(context.Background(), pvc, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}

	return err
}
