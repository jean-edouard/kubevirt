package overhead

import (
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/downwardmetrics"
	"kubevirt.io/kubevirt/pkg/util"
	"kubevirt.io/kubevirt/pkg/util/hardware"
)

const (
	VirtLauncherMonitorOverhead = "25Mi"  // The `ps` RSS for virt-launcher-monitor
	VirtLauncherOverhead        = "100Mi" // The `ps` RSS for the virt-launcher process
	VirtlogdOverhead            = "17Mi"  // The `ps` RSS for virtlogd
	LibvirtdOverhead            = "33Mi"  // The `ps` RSS for libvirtd
	QemuOverhead                = "30Mi"  // The `ps` RSS for qemu, minus the RAM of its (stressed) guest, minus the virtual page table
)

// We need to add this overhead due to potential issues when using exec probes.
// In certain situations depending on things like node size and kernel versions
// the exec probe can cause a significant memory overhead that results in the pod getting OOM killed.
// To prevent this, we add this overhead until we have a better way of doing exec probes.
// The virtProbeTotalAdditionalOverhead is added for the virt-probe binary we use for probing and
// only added once, while the virtProbeOverhead is the general memory consumption of virt-probe
// that we add per added probe.
var virtProbeTotalAdditionalOverhead = resource.MustParse("100Mi")
var virtProbeOverhead = resource.MustParse("10Mi")

func addProbeOverheads(vmi *v1.VirtualMachineInstance, to *resource.Quantity) {
	hasLiveness := addProbeOverhead(vmi.Spec.LivenessProbe, to)
	hasReadiness := addProbeOverhead(vmi.Spec.ReadinessProbe, to)
	if hasLiveness || hasReadiness {
		to.Add(virtProbeTotalAdditionalOverhead)
	}
}

func addProbeOverhead(probe *v1.Probe, to *resource.Quantity) bool {
	if probe != nil && probe.Exec != nil {
		to.Add(virtProbeOverhead)
		return true
	}
	return false
}

// GetMemoryOverhead computes the estimation of total
// memory needed for the domain to operate properly.
// This includes the memory needed for the guest and memory
// for Qemu and OS overhead.
//
// The return value is overhead memory quantity
//
// Note: This is the best estimation we were able to come up with
//       and is still not 100% accurate
func GetMemoryOverhead(vmi *v1.VirtualMachineInstance, cpuArch string) *resource.Quantity {
	domain := vmi.Spec.Domain
	vmiMemoryReq := domain.Resources.Requests.Memory()

	overhead := resource.NewScaledQuantity(0, resource.Kilo)

	// Add the memory needed for pagetables (one bit for every 512b of RAM size)
	pagetableMemory := resource.NewScaledQuantity(vmiMemoryReq.ScaledValue(resource.Kilo), resource.Kilo)
	pagetableMemory.Set(pagetableMemory.Value() / 512)
	overhead.Add(*pagetableMemory)

	// Add fixed overhead for KubeVirt components, as seen in a random run, rounded up to the nearest MiB
	// Note: shared libraries are included in the size, so every library is counted (wrongly) as many times as there are
	//   processes using it. However, the extra memory is only in the order of 10MiB and makes for a nice safety margin.
	overhead.Add(resource.MustParse(VirtLauncherMonitorOverhead))
	overhead.Add(resource.MustParse(VirtLauncherOverhead))
	overhead.Add(resource.MustParse(VirtlogdOverhead))
	overhead.Add(resource.MustParse(LibvirtdOverhead))
	overhead.Add(resource.MustParse(QemuOverhead))

	// Add CPU table overhead (8 MiB per vCPU and 8 MiB per IO thread)
	// overhead per vcpu in MiB
	coresMemory := resource.MustParse("8Mi")
	var vcpus int64
	if domain.CPU != nil {
		vcpus = hardware.GetNumberOfVCPUs(domain.CPU)
	} else {
		// Currently, a default guest CPU topology is set by the API webhook mutator, if not set by a user.
		// However, this wasn't always the case.
		// In case when the guest topology isn't set, take value from resources request or limits.
		resources := vmi.Spec.Domain.Resources
		if cpuLimit, ok := resources.Limits[k8sv1.ResourceCPU]; ok {
			vcpus = cpuLimit.Value()
		} else if cpuRequests, ok := resources.Requests[k8sv1.ResourceCPU]; ok {
			vcpus = cpuRequests.Value()
		}
	}

	// if neither CPU topology nor request or limits provided, set vcpus to 1
	if vcpus < 1 {
		vcpus = 1
	}
	value := coresMemory.Value() * vcpus
	coresMemory = *resource.NewQuantity(value, coresMemory.Format)
	overhead.Add(coresMemory)

	// static overhead for IOThread
	overhead.Add(resource.MustParse("8Mi"))

	// Add video RAM overhead
	if domain.Devices.AutoattachGraphicsDevice == nil || *domain.Devices.AutoattachGraphicsDevice == true {
		overhead.Add(resource.MustParse("16Mi"))
	}

	// When use uefi boot on aarch64 with edk2 package, qemu will create 2 pflash(64Mi each, 128Mi in total)
	// it should be considered for memory overhead
	// Additional information can be found here: https://github.com/qemu/qemu/blob/master/hw/arm/virt.c#L120
	if cpuArch == "arm64" {
		overhead.Add(resource.MustParse("128Mi"))
	}

	// Additional overhead of 1G for VFIO devices. VFIO requires all guest RAM to be locked
	// in addition to MMIO memory space to allow DMA. 1G is often the size of reserved MMIO space on x86 systems.
	// Additial information can be found here: https://www.redhat.com/archives/libvir-list/2015-November/msg00329.html
	if util.IsVFIOVMI(vmi) {
		overhead.Add(resource.MustParse("1Gi"))
	}

	// DownardMetrics volumes are using emptyDirs backed by memory.
	// the max. disk size is only 256Ki.
	if downwardmetrics.HasDownwardMetricDisk(vmi) {
		overhead.Add(resource.MustParse("1Mi"))
	}

	addProbeOverheads(vmi, overhead)

	// Consider memory overhead for SEV guests.
	// Additional information can be found here: https://libvirt.org/kbase/launch_security_sev.html#memory
	if util.IsSEVVMI(vmi) {
		overhead.Add(resource.MustParse("256Mi"))
	}

	// Having a TPM device will spawn a swtpm process
	// In `ps`, swtpm has VSZ of 53808 and RSS of 3496, so 53Mi should do
	if vmi.Spec.Domain.Devices.TPM != nil {
		overhead.Add(resource.MustParse("53Mi"))
	}

	return overhead
}
