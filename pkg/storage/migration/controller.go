package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	virtv1 "kubevirt.io/api/core/v1"
	virtstoragev1alpha1 "kubevirt.io/api/storage/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"

	k8score "k8s.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/controller"
)

const failedToProcessDeleteNotificationErrMsg = "Failed to process delete notification"
const labelStorageMigration = "storage.kubevirt.io/migration"

type StorageMigrationController struct {
	Queue                    workqueue.RateLimitingInterface
	clientset                kubecli.KubevirtClient
	storageMigrationInformer cache.SharedIndexInformer
	vmiInformer              cache.SharedIndexInformer
	migrationInformer        cache.SharedIndexInformer
	vmInformer               cache.SharedIndexInformer
	expectations             *controller.UIDTrackingControllerExpectations
}

func NewStorageMigrationController(clientset kubecli.KubevirtClient, storageMigrationInformer cache.SharedIndexInformer, migrationInformer cache.SharedIndexInformer, vmiInformer cache.SharedIndexInformer, vmInformer cache.SharedIndexInformer) (*StorageMigrationController, error) {
	c := &StorageMigrationController{
		Queue:                    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "virt-controller-storage-migration"),
		clientset:                clientset,
		storageMigrationInformer: storageMigrationInformer,
		vmiInformer:              vmiInformer,
		vmInformer:               vmInformer,
		migrationInformer:        migrationInformer,
	}

	_, err := c.storageMigrationInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addStorageMigration,
		DeleteFunc: c.deleteStorageMigration,
		UpdateFunc: c.updateStorageMigration,
	})
	if err != nil {
		return nil, err
	}

	//	_, err = c.vmiInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
	//		AddFunc:    c.addVMI,
	//		DeleteFunc: c.deleteVMI,
	//		UpdateFunc: c.updateVMI,
	//	})
	//	if err != nil {
	//		return nil, err
	//	}

	_, err = c.migrationInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addMigration,
		DeleteFunc: c.deleteMigration,
		UpdateFunc: c.updateMigration,
	})
	if err != nil {
		return nil, err
	}

	//	_, err = c.vmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
	//		DeleteFunc: c.deleteVM,
	//	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *StorageMigrationController) Run(threadiness int, stopCh <-chan struct{}) {
	defer controller.HandlePanic()
	defer c.Queue.ShutDown()
	log.Log.Info("Starting StorageMigrationController controller.")

	// Wait for cache sync before we start the controller
	cache.WaitForCacheSync(stopCh, c.storageMigrationInformer.HasSynced)

	// Start the actual work
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	log.Log.Info("Stopping StorageMigrationController controller.")
}

func (c *StorageMigrationController) runWorker() {
	for c.Execute() {
	}
}

func (c *StorageMigrationController) Execute() bool {
	key, quit := c.Queue.Get()
	if quit {
		return false
	}
	defer c.Queue.Done(key)
	if err := c.execute(key.(string)); err != nil {
		log.Log.Reason(err).Infof("re-enqueuing StorageMigration %v", key)
		c.Queue.AddRateLimited(key)
	} else {
		log.Log.V(4).Infof("processed StorageMigration %v", key)
		c.Queue.Forget(key)
	}
	return true
}

func (c *StorageMigrationController) triggerVirtualMachineInstanceMigration(migVols []virtstoragev1alpha1.MigratedVolume, vmiName, migName, ns string) error {
	vmiObj, vmiExists, err := c.vmiInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, vmiName))
	if err != nil {
		return err
	}
	if !vmiExists {
		return fmt.Errorf("VMI %s for the migration %s doesn't existed", vmiName, migName)
	}
	vmi := vmiObj.(*virtv1.VirtualMachineInstance)
	// Update the VMI status with the migrate volumes
	c.updateVMIStatusWithMigratedDisksPatch(migVols, vmi)

	// Create VirtualMachineiMigration object
	vmiMig := &virtv1.VirtualMachineInstanceMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   migName,
			Labels: map[string]string{labelStorageMigration: migName},
		},
		Spec: virtv1.VirtualMachineInstanceMigrationSpec{
			VMIName: vmiName,
		},
	}
	_, err = c.clientset.VirtualMachineInstanceMigration(ns).Create(vmiMig, &metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (c *StorageMigrationController) updateStatusStorageMigration(sm *virtstoragev1alpha1.StorageMigration, vmiMig *virtv1.VirtualMachineInstanceMigration) error {
	var err error
	found := false
	smCopy := sm.DeepCopy()
	if vmiMig.Status.MigrationState == nil {
		return nil
	}
	if smCopy.Status == nil {
		smCopy.Status = &virtstoragev1alpha1.StorageMigrationStatus{}
	}

	for _, state := range smCopy.Status.StorageMigrationStates {
		if state.VirtualMachineMigrationName == vmiMig.Spec.VMIName {
			found = true
			state.Completed = vmiMig.Status.MigrationState.Completed
			state.Failed = vmiMig.Status.MigrationState.Failed
		}
	}
	// First update for the storage migration for this VMI
	if !found {
		smCopy.Status.StorageMigrationStates = append(smCopy.Status.StorageMigrationStates, virtstoragev1alpha1.StorageMigrationState{
			VirtualMachineMigrationName: vmiMig.Spec.VMIName,
			MigratedVolume:              sm.Spec.MigratedVolume,
			Completed:                   vmiMig.Status.MigrationState.Completed,
			Failed:                      vmiMig.Status.MigrationState.Failed,
		})
	}
	if smCopy, err = c.clientset.StorageMigration(sm.ObjectMeta.Namespace).Update(context.Background(), smCopy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed updating storage migration %s: %v", sm.Name,
			err)
	}
	sm = smCopy.DeepCopy()

	return nil
}

func (c *StorageMigrationController) updateVMIStatusWithMigratedDisksPatch(migratedVolumes []virtstoragev1alpha1.MigratedVolume, vmi *virtv1.VirtualMachineInstance) error {
	var ops []string
	vmiCopy := vmi.DeepCopy()
	// Always reinitialized the migrated disks
	vmiCopy.Status.MigratedVolumes = []virtstoragev1alpha1.MigratedVolume{}
	for _, d := range migratedVolumes {
		vmiCopy.Status.MigratedVolumes = append(vmiCopy.Status.MigratedVolumes,
			virtstoragev1alpha1.MigratedVolume{
				SourcePvc:      d.SourcePvc,
				DestinationPvc: d.DestinationPvc})

	}
	if !equality.Semantic.DeepEqual(vmi.Status, vmiCopy.Status) {
		newState, err := json.Marshal(vmiCopy.Status)
		if err != nil {
			return err
		}

		oldState, err := json.Marshal(vmi.Status)
		if err != nil {
			return err
		}
		ops = append(ops, fmt.Sprintf(`{ "op": "test", "path": "/status", "value": %s }`, string(oldState)))
		ops = append(ops, fmt.Sprintf(`{ "op": "replace", "path": "/status", "value": %s }`, string(newState)))
		_, err = c.clientset.VirtualMachineInstance(vmi.Namespace).Patch(context.Background(), vmi.Name, types.JSONPatchType, controller.GeneratePatchBytes(ops), &metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func replaceSourceVolswithDestinationVolVMI(storageMig *virtstoragev1alpha1.StorageMigration, vmi *virtv1.VirtualMachineInstance) error {
	replaceVol := make(map[string]string)
	for _, v := range storageMig.Spec.MigratedVolume {
		replaceVol[v.SourcePvc] = v.DestinationPvc
	}

	for i, v := range vmi.Spec.Volumes {
		var claim string
		switch {
		case v.VolumeSource.PersistentVolumeClaim != nil:
			claim = v.VolumeSource.PersistentVolumeClaim.ClaimName
		case v.VolumeSource.DataVolume != nil:
			claim = v.VolumeSource.DataVolume.Name
		default:
			continue
		}

		if dest, ok := replaceVol[claim]; ok {
			switch {
			case v.VolumeSource.PersistentVolumeClaim != nil:
				vmi.Spec.Volumes[i].VolumeSource.PersistentVolumeClaim.ClaimName = dest
			case v.VolumeSource.DataVolume != nil:
				vmi.Spec.Volumes[i].VolumeSource.PersistentVolumeClaim = &virtv1.PersistentVolumeClaimVolumeSource{
					PersistentVolumeClaimVolumeSource: k8score.PersistentVolumeClaimVolumeSource{
						ClaimName: dest,
					},
				}
				vmi.Spec.Volumes[i].VolumeSource.DataVolume = nil
			}
			delete(replaceVol, claim)
		}
	}
	if len(replaceVol) != 0 {
		return fmt.Errorf("failed to replace the source volumes with the destination volumes in the VMI")
	}
	return nil
}

func (c *StorageMigrationController) updateVMIWithMigrationVolumes(vmi *virtv1.VirtualMachineInstance, storageMig *virtstoragev1alpha1.StorageMigration) error {
	vmiCopy := vmi.DeepCopy()
	if err := replaceSourceVolswithDestinationVolVMI(storageMig, vmiCopy); err != nil {
		return err
	}
	if _, err := c.clientset.VirtualMachineInstance(vmi.ObjectMeta.Namespace).Update(context.Background(), vmiCopy); err != nil {
		return fmt.Errorf("failed updating migrated disks: %v", err)
	}
	vmi = vmiCopy.DeepCopy()
	return nil
}

func getVolName(v *virtv1.Volume) string {
	var claim string
	switch {
	case v.VolumeSource.PersistentVolumeClaim != nil:
		claim = v.VolumeSource.PersistentVolumeClaim.ClaimName
	case v.VolumeSource.DataVolume != nil:
		claim = v.VolumeSource.DataVolume.Name
	}
	return claim
}

func (c *StorageMigrationController) replaceSourceVolswithDestinationVolVM(vm *virtv1.VirtualMachine, vmi *virtv1.VirtualMachineInstance) error {
	migrateVolMap := make(map[string]string)
	volVmi := make(map[string]bool)
	if vmi == nil {
		return nil
	}
	if vmi.Status.MigrationState == nil {
		return nil
	}
	for _, v := range vmi.Status.MigratedVolumes {
		migrateVolMap[v.SourcePvc] = v.DestinationPvc
	}
	for _, v := range vmi.Spec.Volumes {
		if name := getVolName(&v); name != "" {
			volVmi[name] = true
		}
	}
	for k, v := range vm.Spec.Template.Spec.Volumes {
		if name := getVolName(&v); name != "" {
			// The volume to update in the VM needs to be one of the migrate
			// volume AND already have been changed in the VMI spec
			repName, okMig := migrateVolMap[name]
			_, okVMI := volVmi[name]
			if okMig && okVMI {
				switch {
				case v.VolumeSource.PersistentVolumeClaim != nil:
					vm.Spec.Template.Spec.Volumes[k].VolumeSource.PersistentVolumeClaim.ClaimName = repName
				case v.VolumeSource.DataVolume != nil:
					vm.Spec.Template.Spec.Volumes[k].VolumeSource.PersistentVolumeClaim = &virtv1.PersistentVolumeClaimVolumeSource{
						PersistentVolumeClaimVolumeSource: k8score.PersistentVolumeClaimVolumeSource{
							ClaimName: repName,
						},
					}
					vm.Spec.Template.Spec.Volumes[k].VolumeSource.DataVolume = nil
				}
			}
		}
	}
	return nil
}

func (c *StorageMigrationController) updateVMWithMigrationVolumes(vm *virtv1.VirtualMachine, vmi *virtv1.VirtualMachineInstance) error {
	vmCopy := vm.DeepCopy()
	if err := c.replaceSourceVolswithDestinationVolVM(vmCopy, vmi); err != nil {
		return err
	}
	if _, err := c.clientset.VirtualMachine(vm.ObjectMeta.Namespace).Update(context.Background(), vmCopy); err != nil {
		return fmt.Errorf("failed updating migrated disks: %v", err)
	}
	vm = vmCopy.DeepCopy()
	return nil
}

func (c *StorageMigrationController) groupVolumesPerVMI(sm *virtstoragev1alpha1.StorageMigration) (map[string][]virtstoragev1alpha1.MigratedVolume, error) {
	migrVolPerVMI := make(map[string][]virtstoragev1alpha1.MigratedVolume)
	vmiList, err := c.clientset.VirtualMachineInstance(sm.Namespace).List(context.Background(), &metav1.ListOptions{})
	if err != nil {
		return migrVolPerVMI, fmt.Errorf("failed to get VMIs: %v", err)
	}
	vols := make(map[string]string)
	for _, volMigr := range sm.Spec.MigratedVolume {
		vols[volMigr.SourcePvc] = volMigr.DestinationPvc
	}

	for _, vmi := range vmiList.Items {
		var migrVols []virtstoragev1alpha1.MigratedVolume
		for _, v := range vmi.Spec.Volumes {
			var name string
			switch {
			case v.VolumeSource.DataVolume != nil:
				name = v.VolumeSource.DataVolume.Name
			case v.VolumeSource.PersistentVolumeClaim != nil:
				name = v.VolumeSource.PersistentVolumeClaim.PersistentVolumeClaimVolumeSource.ClaimName
			default:
				continue
			}
			if dst, ok := vols[name]; ok {
				migrVols = append(migrVols, virtstoragev1alpha1.MigratedVolume{
					SourcePvc:      name,
					DestinationPvc: dst,
				})
			}
		}
		if len(migrVols) > 0 {
			migrVolPerVMI[vmi.Name] = migrVols
		}
	}
	return migrVolPerVMI, nil
}

func (c *StorageMigrationController) execute(key string) error {
	var err error
	obj, exists, err := c.storageMigrationInformer.GetStore().GetByKey(key)
	if err != nil {
		return nil
	}
	if !exists {
		c.expectations.DeleteExpectations(key)
		return nil
	}
	sm := obj.(*virtstoragev1alpha1.StorageMigration)

	logger := log.Log.Object(sm)
	logger.V(1).Infof("Start processing storage class migration: %s", sm.Name)
	// this must be first step in execution. Writing the object
	// when api version changes ensures our api stored version is updated.
	if !controller.ObservedLatestApiVersionAnnotation(sm) {
		smCopy := sm.DeepCopy()
		controller.SetLatestApiVersionAnnotation(smCopy)
		_, err = c.clientset.StorageMigration(sm.Namespace).Update(context.TODO(), smCopy, metav1.UpdateOptions{})
		return err
	}
	migrVolPerVMI, err := c.groupVolumesPerVMI(sm)
	if err != nil {
		return err
	}
	for vmi, volMigr := range migrVolPerVMI {
		if err = c.executeStorageMigPerVMI(sm, volMigr, vmi); err != nil {
			logger.Object(sm).Reason(err).Errorf("Failed to migrate the storage for VMI %s", vmi)
			err = fmt.Errorf("One of the storage migration for sm %s failed", sm.Name)
		}
	}
	return err
}

// TODO: replace this function with errors.Join available from golang 1.20
func joinErrors(errors ...error) error {
	var err error
	for _, e := range errors {
		if e == nil {
			continue
		}
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%s: %w", err.Error(), e)
		}
	}
	return err
}

func (c *StorageMigrationController) executeStorageMigPerVMI(sm *virtstoragev1alpha1.StorageMigration, migVols []virtstoragev1alpha1.MigratedVolume, vmiName string) error {
	ns := sm.Namespace
	vmiObj, vmiExists, err := c.vmiInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, vmiName))
	if err != nil {
		return err
	}
	// Update the VMI object with the migrated disks in the status
	if !vmiExists {
		return fmt.Errorf("VMI %s for the storage migration %s", vmiName, sm.Name)
	}
	vmi := vmiObj.(*virtv1.VirtualMachineInstance)

	logger := log.Log.Object(vmi)
	logger.V(1).Infof("Storage migration for volumes of VMI %s", vmi.Name)

	// Check if the migration has already been triggered
	migName := vmiName + "-storage-migration"
	migObj, exists, err := c.migrationInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, migName))
	if err != nil {
		return nil
	}
	// Start the migration if it doesn't exist
	if !exists {
		logger.V(1).Infof("Start migration for VMI %s", vmi.Name)
		return c.triggerVirtualMachineInstanceMigration(migVols, vmiName, migName, ns)
	}

	mig := migObj.(*virtv1.VirtualMachineInstanceMigration)
	if err := c.updateStatusStorageMigration(sm, mig); err != nil {
		return err
	}
	if err := c.updateVMIStatusWithMigratedDisksPatch(migVols, vmi); err != nil {
		return err
	}

	// If the migration completed then update the VMI and VM spec
	if mig.Status.MigrationState != nil && mig.Status.MigrationState.Completed && !mig.Status.MigrationState.Failed {
		logger.V(1).Infof("Migration completed VMI %s update the migrate volumes", vmi.Name)
		c.updateVMIWithMigrationVolumes(vmi, sm)
		// If the VMI has a VM controller, then update the VM spec consequentially
		if len(vmi.ObjectMeta.OwnerReferences) == 1 {
			vmObj, vmExists, err := c.vmInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", ns, vmiName))
			if err != nil {
				return err
			}
			if !vmExists {
				return fmt.Errorf("VM %s for the storage migration doesn't exist", vmiName)
			}
			vm := vmObj.(*virtv1.VirtualMachine)
			c.updateVMWithMigrationVolumes(vm, vmi)

		}
		// Handle source PVC
		var err error
		for _, v := range migVols {
			switch v.ReclaimPolicySourcePvc {
			case virtstoragev1alpha1.DeleteReclaimPolicySourcePvc:
				err1 := c.clientset.CoreV1().PersistentVolumeClaims(ns).Delete(context.TODO(),
					v.SourcePvc, metav1.DeleteOptions{})
				err = joinErrors(err, err1)
				if err1 != nil {
					logger.V(1).Infof("Delete source volume %s", v.SourcePvc)
				}
			case virtstoragev1alpha1.RetainReclaimPolicySourcePvc:
				// Do nothing for the retain policy
				logger.V(1).Infof("Retain source volume %s", v.SourcePvc)
				continue
			default:
				err = joinErrors(err, fmt.Errorf("PVC policy %s not recongnized", v.ReclaimPolicySourcePvc))
			}
		}
	}

	return err
}

func (c *StorageMigrationController) enqueueStorageMigration(obj interface{}) {
	logger := log.Log
	sm := obj.(*virtstoragev1alpha1.StorageMigration)
	key, err := controller.KeyFunc(sm)
	if err != nil {
		logger.Object(sm).Reason(err).Error("Failed to extract key from storage migration.")
		return
	}
	c.Queue.Add(key)
}

func (c *StorageMigrationController) addStorageMigration(obj interface{}) {
	c.enqueueStorageMigration(obj)
}

func (c *StorageMigrationController) deleteStorageMigration(obj interface{}) {
	c.enqueueStorageMigration(obj)
}

func (c *StorageMigrationController) updateStorageMigration(_, curr interface{}) {
	c.enqueueStorageMigration(curr)
}

//func containsVMI(sm *virtstoragev1alpha1.StorageMigration, name string) bool {
//	for _, v := range sm.Spec.MigratedVolume {
//		if v.VMIName == name {
//			return true
//		}
//	}
//	return false
//}

//func (c *StorageMigrationController) listStorageMigrationsMatchingVMI(namespace, name string) ([]*virtstoragev1alpha1.StorageMigration, error) {
//	objs, err := c.storageMigrationInformer.GetIndexer().ByIndex(cache.NamespaceIndex, namespace)
//	if err != nil {
//		return nil, err
//	}
//
//	var migrations []*virtstoragev1alpha1.StorageMigration
//	for _, obj := range objs {
//		migration := obj.(*virtstoragev1alpha1.StorageMigration)
//
//		if containsVMI(migration, name) {
//			migrations = append(migrations, migration)
//		}
//	}
//	return migrations, nil
//}
//
//func (c *StorageMigrationController) addVMI(obj interface{}) {
//	vmi := obj.(*virtv1.VirtualMachineInstance)
//	if vmi.DeletionTimestamp != nil {
//		c.deleteVMI(vmi)
//		return
//	}
//
//	migrations, err := c.listStorageMigrationsMatchingVMI(vmi.Namespace, vmi.Name)
//	if err != nil {
//		return
//	}
//	for _, migration := range migrations {
//		c.enqueueStorageMigration(migration)
//	}
//}
//
//func (c *StorageMigrationController) updateVMI(old, cur interface{}) {
//	curVMI := cur.(*virtv1.VirtualMachineInstance)
//	oldVMI := old.(*virtv1.VirtualMachineInstance)
//	if curVMI.ResourceVersion == oldVMI.ResourceVersion {
//		return
//	}
//	labelChanged := !equality.Semantic.DeepEqual(curVMI.Labels, oldVMI.Labels)
//	if curVMI.DeletionTimestamp != nil {
//		// having a DataVOlume marked for deletion is enough
//		// to count as a deletion expectation
//		c.deleteVMI(curVMI)
//		if labelChanged {
//			// we don't need to check the oldVMI.DeletionTimestamp
//			// because DeletionTimestamp cannot be unset.
//			c.deleteVMI(oldVMI)
//		}
//		return
//	}
//
//	migrations, err := c.listStorageMigrationsMatchingVMI(curVMI.Namespace, curVMI.Name)
//	if err != nil {
//		log.Log.Object(curVMI).Errorf("Error encountered during datavolume update: %v", err)
//		return
//	}
//	for _, migration := range migrations {
//		log.Log.V(4).Object(curVMI).Infof("vmi updated for storage migration %s", migration.Name)
//		c.enqueueStorageMigration(migration)
//	}
//}
//
//func (c *StorageMigrationController) deleteVMI(obj interface{}) {
//	vmi, ok := obj.(*virtv1.VirtualMachineInstance)
//	// When a delete is dropped, the relist will notice a vmi in the store not
//	// in the list, leading to the insertion of a tombstone object which contains
//	// the deleted key/value. Note that this value might be stale. If the vmi
//	// changed labels the new vmi will not be woken up till the periodic resync.
//	if !ok {
//		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
//		if !ok {
//			log.Log.Reason(fmt.Errorf("couldn't get object from tombstone %+v", obj)).Error(failedToProcessDeleteNotificationErrMsg)
//			return
//		}
//		vmi, ok = tombstone.Obj.(*virtv1.VirtualMachineInstance)
//		if !ok {
//			log.Log.Reason(fmt.Errorf("tombstone contained object that is not a vmi %#v", obj)).Error(failedToProcessDeleteNotificationErrMsg)
//			return
//		}
//	}
//	migrations, err := c.listStorageMigrationsMatchingVMI(vmi.Namespace, vmi.Name)
//	if err != nil {
//		return
//	}
//	for _, migration := range migrations {
//		log.Log.V(4).Object(vmi).Infof("vmi deleted for storage migration %s", migration.Name)
//		c.enqueueStorageMigration(migration)
//	}
//}

func (c *StorageMigrationController) checkAndEnqueuStorageMigration(obj interface{}) {
	mig := obj.(*virtv1.VirtualMachineInstanceMigration)
	smName, ok := mig.ObjectMeta.Labels[labelStorageMigration]
	if !ok {
		return
	}
	smObj, exists, err := c.storageMigrationInformer.GetStore().GetByKey(mig.Namespace + "/" + smName)
	if err != nil {
		return
	}
	if !exists {
		return
	}
	sm := smObj.(*virtstoragev1alpha1.StorageMigration)
	c.enqueueStorageMigration(sm)
}

func (c *StorageMigrationController) addMigration(obj interface{}) {
	c.checkAndEnqueuStorageMigration(obj)
}

func (c *StorageMigrationController) deleteMigration(obj interface{}) {
	c.checkAndEnqueuStorageMigration(obj)
}

func (c *StorageMigrationController) updateMigration(_, curr interface{}) {
	c.checkAndEnqueuStorageMigration(curr)
}

//func (c *StorageMigrationController) deleteVM(obj interface{}) {
//	vm := obj.(*virtv1.VirtualMachine)
//	migrations, err := c.listStorageMigrationsMatchingVMI(vm.Namespace, vm.Name)
//	if err != nil {
//		return
//	}
//	for _, migration := range migrations {
//		c.enqueueStorageMigration(migration)
//	}
//}
