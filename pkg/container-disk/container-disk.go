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
 * Copyright 2017 Red Hat, Inc.
 *
 */

package containerdisk

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	kubev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	ephemeraldisk "kubevirt.io/kubevirt/pkg/ephemeral-disk"

	v1 "kubevirt.io/client-go/api/v1"
	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
	"kubevirt.io/kubevirt/pkg/util"
)

var containerDiskOwner = "qemu"

var podsBaseDir = "/var/lib/kubelet/pods"

var mountBaseDir = filepath.Join(util.VirtShareDir, "/container-disks")

func GetLegacyVolumeMountDirOnHost(vmi *v1.VirtualMachineInstance) string {
	return filepath.Join(mountBaseDir, string(vmi.UID))
}

func GetVolumeMountDirOnGuest(vmi *v1.VirtualMachineInstance) string {
	return filepath.Join(mountBaseDir, string(vmi.UID))
}

func GetVolumeMountDirOnHost(vmi *v1.VirtualMachineInstance) (string, bool, error) {
	for podUID, _ := range vmi.Status.ActivePods {
		basepath := fmt.Sprintf("%s/%s/volumes/kubernetes.io~empty-dir/container-disks", podsBaseDir, string(podUID))
		exists, err := diskutils.FileExists(basepath)
		if err != nil {
			return "", false, err
		} else if exists {
			return basepath, true, nil
		}
	}
	return "", false, nil
}

func GetDiskTargetPathFromHostView(vmi *v1.VirtualMachineInstance, volumeIndex int) (string, error) {
	basepath, found, err := GetVolumeMountDirOnHost(vmi)
	if err != nil {
		return "", err
	} else if !found {
		return "", fmt.Errorf("container disk volume for vmi not found")
	}

	return fmt.Sprintf("%s/disk_%d.img", basepath, volumeIndex), nil
}

func GetDiskTargetPathFromLauncherView(volumeIndex int) string {
	return filepath.Join(mountBaseDir, fmt.Sprintf("disk_%d.img", volumeIndex))
}

func SetLocalDirectory(dir string) error {
	mountBaseDir = dir
	return os.MkdirAll(dir, 0755)
}

func setPodsDirectory(dir string) error {
	podsBaseDir = dir
	return os.MkdirAll(dir, 0755)
}

// The unit test suite uses this function
func setLocalDataOwner(user string) {
	containerDiskOwner = user
}

// GetDiskTargetPartFromLauncherView returns (path to disk image, image type, and error)
func GetDiskTargetPartFromLauncherView(volumeIndex int) (string, error) {

	path := GetDiskTargetPathFromLauncherView(volumeIndex)
	exists, err := diskutils.FileExists(path)
	if err != nil {
		return "", err
	} else if exists {
		return path, nil
	}

	return "", fmt.Errorf("no supported file disk found for volume with index %d", volumeIndex)
}

func GetSocketPathFromHostView(vmi *v1.VirtualMachineInstance, volumeIndex int) (string, error) {
	for podUID, _ := range vmi.Status.ActivePods {
		basepath := fmt.Sprintf("/pods/%s/volumes/kubernetes.io~empty-dir/container-disks", string(podUID))
		exists, _ := diskutils.FileExists(basepath)
		if exists {
			return filepath.Join(basepath, fmt.Sprintf("disk_%d.sock", volumeIndex)), nil
		}
	}
	return "", fmt.Errorf("container disk socket path not found for vmi")
}

func GetImage(root string, imagePath string) (string, error) {
	fallbackPath := filepath.Join(root, DiskSourceFallbackPath)
	if imagePath != "" {
		imagePath = filepath.Join(root, imagePath)
		if _, err := os.Stat(imagePath); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("No image on path %s", imagePath)
			} else {
				return "", fmt.Errorf("Failed to check if an image exists at %s", imagePath)
			}
		}
	} else {
		files, err := ioutil.ReadDir(fallbackPath)
		if err != nil {
			return "", fmt.Errorf("Failed to check %s for disks: %v", fallbackPath, err)
		}
		if len(files) > 1 {
			return "", fmt.Errorf("More than one file found in folder %s, only one disk is allowed", DiskSourceFallbackPath)
		}
		imagePath = filepath.Join(fallbackPath, files[0].Name())
	}
	return imagePath, nil
}

// The controller uses this function to generate the container
// specs for hosting the container registry disks.
func GenerateContainers(vmi *v1.VirtualMachineInstance, podVolumeName string, binVolumeName string, selinuxType *string) []kubev1.Container {
	var containers []kubev1.Container

	initialDelaySeconds := 1
	timeoutSeconds := 1
	periodSeconds := 1
	successThreshold := 1
	failureThreshold := 5

	// Make VirtualMachineInstance Image Wrapper Containers
	for index, volume := range vmi.Spec.Volumes {
		if volume.ContainerDisk != nil {

			volumeMountDir := GetVolumeMountDirOnGuest(vmi)
			diskContainerName := fmt.Sprintf("volume%s", volume.Name)
			diskContainerImage := volume.ContainerDisk.Image
			resources := kubev1.ResourceRequirements{}
			if vmi.IsCPUDedicated() || vmi.WantsToHaveQOSGuaranteed() {
				resources.Limits = make(kubev1.ResourceList)
				resources.Limits[kubev1.ResourceCPU] = resource.MustParse("10m")
				resources.Limits[kubev1.ResourceMemory] = resource.MustParse("40M")
				resources.Requests = make(kubev1.ResourceList)
				resources.Requests[kubev1.ResourceCPU] = resource.MustParse("10m")
				resources.Requests[kubev1.ResourceMemory] = resource.MustParse("40M")
			} else {
				resources.Limits = make(kubev1.ResourceList)
				resources.Limits[kubev1.ResourceCPU] = resource.MustParse("100m")
				resources.Limits[kubev1.ResourceMemory] = resource.MustParse("40M")
				resources.Requests = make(kubev1.ResourceList)
				resources.Requests[kubev1.ResourceCPU] = resource.MustParse("10m")
				resources.Requests[kubev1.ResourceMemory] = resource.MustParse("1M")
			}
			container := kubev1.Container{
				Name:            diskContainerName,
				Image:           diskContainerImage,
				ImagePullPolicy: volume.ContainerDisk.ImagePullPolicy,
				Command:         []string{"/usr/bin/container-disk"},
				Args:            []string{"--copy-path", volumeMountDir + "/disk_" + strconv.Itoa(index)},
				VolumeMounts: []kubev1.VolumeMount{
					{
						Name:      podVolumeName,
						MountPath: volumeMountDir,
					},
					{
						Name:      binVolumeName,
						MountPath: "/usr/bin",
					},
				},
				Resources: resources,

				// The readiness probes ensure the volume coversion and copy finished
				// before the container is marked as "Ready: True"
				ReadinessProbe: &kubev1.Probe{
					Handler: kubev1.Handler{
						Exec: &kubev1.ExecAction{
							Command: []string{
								"/usr/bin/container-disk",
								"--health-check",
							},
						},
					},
					InitialDelaySeconds: int32(initialDelaySeconds),
					PeriodSeconds:       int32(periodSeconds),
					TimeoutSeconds:      int32(timeoutSeconds),
					SuccessThreshold:    int32(successThreshold),
					FailureThreshold:    int32(failureThreshold),
				},
			}
			if selinuxType != nil {
				// If we're here, a custom SELinux type will be set on the virt-launcher pod.
				// That has the interesting side effect of generating different categories
				//   for each container, instead of using the same for all as usual.
				// We can't have different categories than the compute container, so we force our
				//   level to be just "s0", since setting no categories means all can read/write.
				container.SecurityContext = &kubev1.SecurityContext{
					SELinuxOptions: &kubev1.SELinuxOptions{
						Type:  *selinuxType,
						Level: "s0",
					},
				}
			}

			containers = append(containers, container)
		}
	}
	return containers
}

func CreateEphemeralImages(vmi *v1.VirtualMachineInstance) error {
	// The domain is setup to use the COW image instead of the base image. What we have
	// to do here is only create the image where the domain expects it (GetDiskTargetPartFromLauncherView)
	// for each disk that requires it.

	for i, volume := range vmi.Spec.Volumes {
		if volume.VolumeSource.ContainerDisk != nil {
			if backingFile, err := GetDiskTargetPartFromLauncherView(i); err != nil {
				return err
			} else if err := ephemeraldisk.CreateBackedImageForVolume(volume, backingFile); err != nil {
				return err
			}
		}
	}

	return nil
}
