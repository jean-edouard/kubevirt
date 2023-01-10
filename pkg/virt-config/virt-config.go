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
 * Copyright 2017, 2018 Red Hat, Inc.
 *
 */

package virtconfig

/*
 This module is intended for exposing the virtualization configuration that is available at the cluster-level and its default settings.
*/

import (
	"fmt"

	"kubevirt.io/client-go/log"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "kubevirt.io/api/core/v1"
)

const (
	ParallelOutboundMigrationsPerNodeDefault uint32 = 2
	ParallelMigrationsPerClusterDefault      uint32 = 5
	BandwithPerMigrationDefault                     = "0Mi"
	MigrationAllowAutoConverge               bool   = false
	MigrationAllowPostCopy                   bool   = false
	MigrationProgressTimeout                 int64  = 150
	MigrationCompletionTimeoutPerGiB         int64  = 800
	DefaultAMD64MachineType                         = "q35"
	DefaultPPC64LEMachineType                       = "pseries"
	DefaultAARCH64MachineType                       = "virt"
	DefaultCPURequest                               = "100m"
	DefaultMemoryOvercommit                         = 100
	DefaultAMD64EmulatedMachines                    = "q35*,pc-q35*"
	DefaultPPC64LEEmulatedMachines                  = "pseries*"
	DefaultAARCH64EmulatedMachines                  = "virt*"
	DefaultLessPVCSpaceToleration                   = 10
	DefaultMinimumReservePVCBytes                   = 131072
	DefaultNodeSelectors                            = ""
	DefaultNetworkInterface                         = "bridge"
	DefaultImagePullPolicy                          = k8sv1.PullIfNotPresent
	DefaultAllowEmulation                           = false
	DefaultUnsafeMigrationOverride                  = false
	DefaultPermitSlirpInterface                     = false
	SmbiosConfigDefaultFamily                       = "KubeVirt"
	SmbiosConfigDefaultManufacturer                 = "KubeVirt"
	SmbiosConfigDefaultProduct                      = "None"
	DefaultPermitBridgeInterfaceOnPodNetwork        = true
	DefaultSELinuxLauncherType                      = ""
	SupportedGuestAgentVersions                     = "2.*,3.*,4.*,5.*"
	DefaultARCHOVMFPath                             = "/usr/share/OVMF"
	DefaultAARCH64OVMFPath                          = "/usr/share/AAVMF"
	DefaultMemBalloonStatsPeriod             uint32 = 10
	DefaultCPUAllocationRatio                       = 10
	DefaultDiskVerificationMemoryLimitMBytes        = 1700
	DefaultVirtAPILogVerbosity                      = 2
	DefaultVirtControllerLogVerbosity               = 2
	DefaultVirtHandlerLogVerbosity                  = 2
	DefaultVirtLauncherLogVerbosity                 = 2
	DefaultVirtOperatorLogVerbosity                 = 2

	// Default REST configuration settings
	DefaultVirtHandlerQPS         float32 = 5
	DefaultVirtHandlerBurst               = 10
	DefaultVirtControllerQPS      float32 = 200
	DefaultVirtControllerBurst            = 400
	DefaultVirtAPIQPS             float32 = 5
	DefaultVirtAPIBurst                   = 10
	DefaultVirtWebhookClientQPS           = 200
	DefaultVirtWebhookClientBurst         = 400
)

func IsAMD64(arch string) bool {
	if arch == "amd64" {
		return true
	}
	return false
}

func IsARM64(arch string) bool {
	if arch == "arm64" {
		return true
	}
	return false
}

func IsPPC64(arch string) bool {
	if arch == "ppc64le" {
		return true
	}
	return false
}

func (config *ClusterConfig) GetMemBalloonStatsPeriod() uint32 {
	return *config.GetConfig().MemBalloonStatsPeriod
}

func (config *ClusterConfig) AllowEmulation() bool {
	return config.GetConfig().DeveloperConfiguration.UseEmulation
}

func (config *ClusterConfig) GetMigrationConfiguration() *v1.MigrationConfiguration {
	return config.GetConfig().MigrationConfiguration
}

func (config *ClusterConfig) GetImagePullPolicy() (policy k8sv1.PullPolicy) {
	return config.GetConfig().ImagePullPolicy
}

func (config *ClusterConfig) GetResourceVersion() string {
	config.lock.Lock()
	defer config.lock.Unlock()
	return config.lastValidConfigResourceVersion
}

func (config *ClusterConfig) GetMachineType() string {
	return config.GetConfig().MachineType
}

func (config *ClusterConfig) GetCPUModel() string {
	return config.GetConfig().CPUModel
}

func (config *ClusterConfig) GetCPURequest() *resource.Quantity {
	return config.GetConfig().CPURequest
}

func (config *ClusterConfig) GetDiskVerification() *v1.DiskVerification {
	return config.GetConfig().DeveloperConfiguration.DiskVerification
}

func (config *ClusterConfig) GetMemoryOvercommit() int {
	return config.GetConfig().DeveloperConfiguration.MemoryOvercommit
}

func (config *ClusterConfig) GetEmulatedMachines() []string {
	return config.GetConfig().EmulatedMachines
}

func (config *ClusterConfig) GetLessPVCSpaceToleration() int {
	return config.GetConfig().DeveloperConfiguration.LessPVCSpaceToleration
}

func (config *ClusterConfig) GetMinimumReservePVCBytes() uint64 {
	return config.GetConfig().DeveloperConfiguration.MinimumReservePVCBytes
}

func (config *ClusterConfig) GetNodeSelectors() map[string]string {
	return config.GetConfig().DeveloperConfiguration.NodeSelectors
}

func (config *ClusterConfig) GetDefaultNetworkInterface() string {
	return config.GetConfig().NetworkConfiguration.NetworkInterface
}

func (config *ClusterConfig) SetVMIDefaultNetworkInterface(vmi *v1.VirtualMachineInstance) error {
	autoAttach := vmi.Spec.Domain.Devices.AutoattachPodInterface
	if autoAttach != nil && *autoAttach == false {
		return nil
	}

	// Override only when nothing is specified
	if len(vmi.Spec.Networks) == 0 && len(vmi.Spec.Domain.Devices.Interfaces) == 0 {
		iface := v1.NetworkInterfaceType(config.GetDefaultNetworkInterface())
		switch iface {
		case v1.BridgeInterface:
			if !config.IsBridgeInterfaceOnPodNetworkEnabled() {
				return fmt.Errorf("Bridge interface is not enabled in kubevirt-config")
			}
			vmi.Spec.Domain.Devices.Interfaces = []v1.Interface{*v1.DefaultBridgeNetworkInterface()}
		case v1.MasqueradeInterface:
			vmi.Spec.Domain.Devices.Interfaces = []v1.Interface{*v1.DefaultMasqueradeNetworkInterface()}
		case v1.SlirpInterface:
			if !config.IsSlirpInterfaceEnabled() {
				return fmt.Errorf("Slirp interface is not enabled in kubevirt-config")
			}
			defaultIface := v1.DefaultSlirpNetworkInterface()
			vmi.Spec.Domain.Devices.Interfaces = []v1.Interface{*defaultIface}
		}

		vmi.Spec.Networks = []v1.Network{*v1.DefaultPodNetwork()}
	}
	return nil
}

func (config *ClusterConfig) IsSlirpInterfaceEnabled() bool {
	return *config.GetConfig().NetworkConfiguration.PermitSlirpInterface
}

func (config *ClusterConfig) GetSMBIOS() *v1.SMBiosConfiguration {
	return config.GetConfig().SMBIOSConfig
}

func (config *ClusterConfig) IsBridgeInterfaceOnPodNetworkEnabled() bool {
	return *config.GetConfig().NetworkConfiguration.PermitBridgeInterfaceOnPodNetwork
}

func (config *ClusterConfig) GetDefaultClusterConfig() *v1.KubeVirtConfiguration {
	return config.defaultConfig
}

func (config *ClusterConfig) GetSELinuxLauncherType() string {
	return config.GetConfig().SELinuxLauncherType
}

func (config *ClusterConfig) GetDefaultRuntimeClass() string {
	return config.GetConfig().DefaultRuntimeClass
}

func (config *ClusterConfig) GetSupportedAgentVersions() []string {
	return config.GetConfig().SupportedGuestAgentVersions
}

func (config *ClusterConfig) GetOVMFPath() string {
	return config.GetConfig().OVMFPath
}

func (config *ClusterConfig) GetCPUAllocationRatio() int {
	return config.GetConfig().DeveloperConfiguration.CPUAllocationRatio
}

func (config *ClusterConfig) GetMinimumClusterTSCFrequency() *int64 {
	return config.GetConfig().DeveloperConfiguration.MinimumClusterTSCFrequency
}

func (config *ClusterConfig) GetPermittedHostDevices() *v1.PermittedHostDevices {
	return config.GetConfig().PermittedHostDevices
}

func canSelectNode(nodeSelector map[string]string, node *k8sv1.Node) bool {
	for key, val := range nodeSelector {
		labelValue, exist := node.Labels[key]
		if !exist || val != labelValue {
			return false
		}
	}
	return true
}

func (config *ClusterConfig) GetDesiredMDEVTypes(node *k8sv1.Node) []string {
	mdevTypesConf := config.GetConfig().MediatedDevicesConfiguration
	if mdevTypesConf == nil {
		return []string{}
	}
	nodeMdevConf := mdevTypesConf.NodeMediatedDeviceTypes
	if nodeMdevConf != nil {
		mdevTypesMap := make(map[string]struct{})
		for _, nodeConfig := range nodeMdevConf {
			if canSelectNode(nodeConfig.NodeSelector, node) {
				types := nodeConfig.MediatedDeviceTypes
				// Handle deprecated spelling
				if len(types) == 0 {
					types = nodeConfig.MediatedDevicesTypes
				}
				for _, mdevType := range types {
					mdevTypesMap[mdevType] = struct{}{}
				}
			}
		}
		if len(mdevTypesMap) != 0 {
			mdevTypesList := []string{}
			for mdevType, _ := range mdevTypesMap {
				mdevTypesList = append(mdevTypesList, mdevType)
			}
			return mdevTypesList
		}
	}
	// Handle deprecated spelling
	if len(mdevTypesConf.MediatedDeviceTypes) == 0 {
		return mdevTypesConf.MediatedDevicesTypes
	}
	return mdevTypesConf.MediatedDeviceTypes
}

type virtComponent int

const (
	virtHandler virtComponent = iota
	virtApi
	virtController
	virtOperator
	virtLauncher
)

// Gets the component verbosity. nodeName can be empty, then it's ignored.
func (config *ClusterConfig) getComponentVerbosity(component virtComponent, nodeName string) uint {
	logConf := config.GetConfig().DeveloperConfiguration.LogVerbosity

	if nodeName != "" {
		if level := logConf.NodeVerbosity[nodeName]; level != 0 {
			return level
		}
	}

	switch component {
	case virtHandler:
		return logConf.VirtHandler
	case virtApi:
		return logConf.VirtAPI
	case virtController:
		return logConf.VirtController
	case virtOperator:
		return logConf.VirtOperator
	case virtLauncher:
		return logConf.VirtLauncher
	default:
		log.Log.Errorf("getComponentVerbosity called with an unknown virtComponent: %v", component)
		return 0
	}
}

func (config *ClusterConfig) GetVirtHandlerVerbosity(nodeName string) uint {
	return config.getComponentVerbosity(virtHandler, nodeName)
}

func (config *ClusterConfig) GetVirtAPIVerbosity(nodeName string) uint {
	return config.getComponentVerbosity(virtApi, nodeName)
}

func (config *ClusterConfig) GetVirtControllerVerbosity(nodeName string) uint {
	return config.getComponentVerbosity(virtController, nodeName)
}

func (config *ClusterConfig) GetVirtOperatorVerbosity(nodeName string) uint {
	return config.getComponentVerbosity(virtOperator, nodeName)
}

func (config *ClusterConfig) GetVirtLauncherVerbosity() uint {
	return config.getComponentVerbosity(virtLauncher, "")
}

// GetMinCPUModel return minimal cpu which is used in node-labeller
func (config *ClusterConfig) GetMinCPUModel() string {
	return config.GetConfig().MinCPUModel
}

// GetObsoleteCPUModels return slice of obsolete cpus which are used in node-labeller
func (config *ClusterConfig) GetObsoleteCPUModels() map[string]bool {
	return config.GetConfig().ObsoleteCPUModels
}

// GetClusterCPUArch return the CPU architecture in ClusterConfig
func (config *ClusterConfig) GetClusterCPUArch() string {
	return config.cpuArch
}
