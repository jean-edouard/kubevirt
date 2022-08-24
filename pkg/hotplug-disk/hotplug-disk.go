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
 * Copyright 2020 Red Hat, Inc.
 *
 */

package hotplugdisk

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/types"

	"kubevirt.io/kubevirt/pkg/safepath"

	"kubevirt.io/kubevirt/pkg/util"
)

var (
	podsBaseDir = filepath.Join(util.HostRootMount, util.KubeletPodsDir)

	mountBaseDir = filepath.Join(util.VirtShareDir, "/hotplug-disks")

	targetPodBasePath = func(podUID types.UID) string {
		return fmt.Sprintf("%s/volumes/kubernetes.io~empty-dir/hotplug-disks", string(podUID))
	}
)

// GetHotplugTargetPodPathOnHost retrieves the target pod (virt-launcher) path on the host.
func GetHotplugTargetPodPathOnHost(virtlauncherPodUID types.UID) (*safepath.Path, error) {
	podpath := targetPodBasePath(virtlauncherPodUID)
	return safepath.JoinAndResolveWithRelativeRoot("/", filepath.Join(podsBaseDir, podpath))
}

// GetFileSystemDiskTargetPathFromHostView gets the disk image file in the target pod (virt-launcher) on the host.
func GetFileSystemDiskTargetPathFromHostView(virtlauncherPodUID types.UID, volumeName string, create bool) (*safepath.Path, error) {
	targetPath, err := GetHotplugTargetPodPathOnHost(virtlauncherPodUID)
	if err != nil {
		return targetPath, err
	}
	diskName := fmt.Sprintf("%s.img", volumeName)
	if err := safepath.TouchAtNoFollow(targetPath, diskName, 0666); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return safepath.JoinNoFollow(targetPath, diskName)
}

// SetLocalDirectory sets the base directory where disk images will be mounted when hotplugged. File system volumes will be in
// a directory under this, that contains the volume name. block volumes will be in this directory as a block device.
func SetLocalDirectory(dir string) error {
	mountBaseDir = dir
	return os.MkdirAll(dir, 0750)
}

// SetKubeletPodsDirectory sets the base directory of where the kubelet stores its pods.
func SetKubeletPodsDirectory(dir string) {
	podsBaseDir = dir
}
