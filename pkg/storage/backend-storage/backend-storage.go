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
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"kubevirt.io/kubevirt/pkg/apimachinery/patch"

	"kubevirt.io/client-go/log"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"

	"k8s.io/client-go/tools/cache"

	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"

	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
)

const (
	PVCPrefix = "persistent-state-for"
	PVCSize   = "10Mi"
)

func BasePVC(vmi *corev1.VirtualMachineInstance) string {
	return PVCPrefix + "-" + vmi.Name
}

func PVCForVMI(pvcIndexer cache.Store, vmi *corev1.VirtualMachineInstance) *v1.PersistentVolumeClaim {
	var legacyPVC *v1.PersistentVolumeClaim

	objs := pvcIndexer.List()
	for _, obj := range objs {
		pvc := obj.(*v1.PersistentVolumeClaim)
		vmName, found := pvc.Labels[PVCPrefix]
		if found && vmName == vmi.Name {
			return pvc
		}
		if pvc.Name == BasePVC(vmi) && pvc.Namespace == vmi.Namespace {
			legacyPVC = pvc
		}
	}

	if legacyPVC != nil {
		return legacyPVC
	}

	return nil
}

func CurrentPVCName(vmi *corev1.VirtualMachineInstance) string {
	for _, volume := range vmi.Status.VolumeStatus {
		if strings.HasPrefix(volume.Name, BasePVC(vmi)) {
			return volume.Name
		}
	}

	return ""
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

func MigrationHandoff(client kubecli.KubevirtClient, migration *corev1.VirtualMachineInstanceMigration) error {
	if migration == nil || migration.Status.MigrationState == nil ||
		migration.Status.MigrationState.SourcePersistentStatePVCName == "" ||
		migration.Status.MigrationState.TargetPersistentStatePVCName == "" {
		return fmt.Errorf("missing source and/or target PVC name(s)")
	}

	sourcePVC := migration.Status.MigrationState.SourcePersistentStatePVCName
	targetPVC := migration.Status.MigrationState.TargetPersistentStatePVCName

	if sourcePVC == targetPVC {
		// RWX backend-storage, nothing to do
		return nil
	}

	// Let's label the target first, then remove the source.
	// The target might already be labelled if this function was already called for this migration
	target, err := client.CoreV1().PersistentVolumeClaims(migration.Namespace).Get(context.Background(), targetPVC, metav1.GetOptions{})
	if err != nil {
		return err
	}
	labels := target.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	existing, ok := labels[PVCPrefix]
	if !ok || existing != migration.Spec.VMIName {
		if ok && existing != migration.Spec.VMIName {
			return fmt.Errorf("target PVC for %s is already labelled for another VMI: %s", migration.Spec.VMIName, existing)
		}
		labelPatch := patch.New()
		if len(labels) == 0 {
			labels[PVCPrefix] = migration.Spec.VMIName
			labelPatch.AddOption(patch.WithAdd("/metadata/labels", labels))
		} else {
			labelPatch.AddOption(patch.WithReplace("/metadata/labels/"+PVCPrefix, migration.Spec.VMIName))
		}

		labelPatchPayload, err := labelPatch.GeneratePayload()
		if err != nil {
			return fmt.Errorf("failed to generate PVC patch: %v", err)
		}
		_, err = client.CoreV1().PersistentVolumeClaims(migration.Namespace).Patch(context.Background(), targetPVC, types.JSONPatchType, labelPatchPayload, metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("failed to patch PVC: %v", err)
		}
	}

	err = client.CoreV1().PersistentVolumeClaims(migration.Namespace).Delete(context.Background(), sourcePVC, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete PVC: %v", err)
	}

	return nil
}

func MigrationAbort(client kubecli.KubevirtClient, migration *corev1.VirtualMachineInstanceMigration) error {
	if migration == nil || migration.Status.MigrationState == nil ||
		migration.Status.MigrationState.TargetPersistentStatePVCName == "" {
		return nil
	}

	sourcePVC := migration.Status.MigrationState.SourcePersistentStatePVCName
	targetPVC := migration.Status.MigrationState.TargetPersistentStatePVCName

	if sourcePVC == targetPVC {
		// RWX backend-storage, nothing to delete
		return nil
	}

	err := client.CoreV1().PersistentVolumeClaims(migration.Namespace).Delete(context.Background(), targetPVC, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete PVC: %v", err)
	}

	return nil
}

type BackendStorage struct {
	client        kubecli.KubevirtClient
	clusterConfig *virtconfig.ClusterConfig
	scStore       cache.Store
	spStore       cache.Store
	pvcStore      cache.Store
}

func NewBackendStorage(client kubecli.KubevirtClient, clusterConfig *virtconfig.ClusterConfig, scStore cache.Store, spStore cache.Store, pvcStore cache.Store) *BackendStorage {
	return &BackendStorage{
		client:        client,
		clusterConfig: clusterConfig,
		scStore:       scStore,
		spStore:       spStore,
		pvcStore:      pvcStore,
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

func (bs *BackendStorage) getAccessMode(storageClass string, mode v1.PersistentVolumeMode) v1.PersistentVolumeAccessMode {
	// The default access mode should be RWX if the storage class was manually specified.
	// However, if we're using the cluster default storage class, default to access mode RWO.
	accessMode := v1.ReadWriteMany
	if bs.clusterConfig.GetVMStateStorageClass() == "" {
		accessMode = v1.ReadWriteOnce
	}

	// Storage profiles are guaranteed to have the same name as their storage class
	obj, exists, err := bs.spStore.GetByKey(storageClass)
	if err != nil {
		log.Log.Reason(err).Infof("couldn't access storage profiles, defaulting to %s", accessMode)
		return accessMode
	}
	if !exists {
		log.Log.Infof("no storage profile found for %s, defaulting to %s", storageClass, accessMode)
		return accessMode
	}
	storageProfile := obj.(*cdiv1.StorageProfile)

	if storageProfile.Status.ClaimPropertySets == nil || len(storageProfile.Status.ClaimPropertySets) == 0 {
		log.Log.Infof("no ClaimPropertySets in storage profile %s, defaulting to %s", storageProfile.Name, accessMode)
		return accessMode
	}

	foundrwo := false
	for _, property := range storageProfile.Status.ClaimPropertySets {
		if property.VolumeMode == nil || *property.VolumeMode != mode || property.AccessModes == nil {
			continue
		}
		for _, accessMode := range property.AccessModes {
			switch accessMode {
			case v1.ReadWriteMany:
				return v1.ReadWriteMany
			case v1.ReadWriteOnce:
				foundrwo = true
			}
		}
	}
	if foundrwo {
		return v1.ReadWriteOnce
	}

	return accessMode
}

func (bs *BackendStorage) updateVolumeStatus(vmi *corev1.VirtualMachineInstance, pvc *v1.PersistentVolumeClaim) {
	if vmi.Status.VolumeStatus == nil {
		vmi.Status.VolumeStatus = []corev1.VolumeStatus{}
	}
	for i := range vmi.Status.VolumeStatus {
		if vmi.Status.VolumeStatus[i].Name == pvc.Name {
			if vmi.Status.VolumeStatus[i].PersistentVolumeClaimInfo == nil {
				vmi.Status.VolumeStatus[i].PersistentVolumeClaimInfo = &corev1.PersistentVolumeClaimInfo{}
			}
			vmi.Status.VolumeStatus[i].PersistentVolumeClaimInfo.ClaimName = pvc.Name
			vmi.Status.VolumeStatus[i].PersistentVolumeClaimInfo.AccessModes = pvc.Spec.AccessModes
			return
		}
	}
	vmi.Status.VolumeStatus = append(vmi.Status.VolumeStatus, corev1.VolumeStatus{
		Name: pvc.Name,
		PersistentVolumeClaimInfo: &corev1.PersistentVolumeClaimInfo{
			ClaimName:   pvc.Name,
			AccessModes: pvc.Spec.AccessModes,
		},
	})
}

func (bs *BackendStorage) CreateIfNeededAndUpdateVolumeStatus(vmi *corev1.VirtualMachineInstance, migrationTarget bool) (string, error) {
	if !IsBackendStorageNeededForVMI(&vmi.Spec) {
		return "", nil
	}

	// TODO: if the pvc was just created and the pvcStore doesn't yet know about it, we'll create a second PVC...
	// We probably need an API call instead
	pvc := PVCForVMI(bs.pvcStore, vmi)

	if (!migrationTarget && pvc != nil) ||
		(migrationTarget && len(pvc.Status.AccessModes) > 0 && pvc.Status.AccessModes[0] == v1.ReadWriteMany) {
		// We're not a migration target and the PVC already exists, or we are a migration target and the PVC is RWX
		// In both cases, we just return the existing PVC.
		bs.updateVolumeStatus(vmi, pvc)
		return pvc.Name, nil
	}

	storageClass, err := bs.getStorageClass()
	if err != nil {
		return "", err
	}
	mode := v1.PersistentVolumeFilesystem
	accessMode := bs.getAccessMode(storageClass, mode)
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
	pvc = &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    BasePVC(vmi) + "-",
			OwnerReferences: ownerReferences,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{accessMode},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{v1.ResourceStorage: resource.MustParse(PVCSize)},
			},
			StorageClassName: &storageClass,
			VolumeMode:       &mode,
		},
	}
	if !migrationTarget {
		// If the PVC is for a migration target, we'll set the label at the end of the migration
		pvc.Labels = map[string]string{PVCPrefix: vmi.Name}
	}

	pvc, err = bs.client.CoreV1().PersistentVolumeClaims(vmi.Namespace).Create(context.Background(), pvc, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	bs.updateVolumeStatus(vmi, pvc)

	return pvc.Name, nil
}

// IsPVCReady returns true if either:
// - No PVC is needed for the VMI since it doesn't use backend storage
// - The backend storage PVC is bound
// - The backend storage PVC is pending uses a WaitForFirstConsumer storage class
func (bs *BackendStorage) IsPVCReady(vmi *corev1.VirtualMachineInstance, pvcName string) (bool, error) {
	if !IsBackendStorageNeededForVMI(&vmi.Spec) {
		return true, nil
	}

	obj, exists, err := bs.pvcStore.GetByKey(vmi.Namespace + "/" + pvcName)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, fmt.Errorf("pvc %s not found in namespace %s", pvcName, vmi.Namespace)
	}
	pvc := obj.(*v1.PersistentVolumeClaim)

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
