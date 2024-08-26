package tests_test

import (
	"context"
	"fmt"
	"time"

	expect "github.com/google/goexpect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/libvmi"
	libvmici "kubevirt.io/kubevirt/pkg/libvmi/cloudinit"
	"kubevirt.io/kubevirt/tests/console"
	"kubevirt.io/kubevirt/tests/decorators"
	"kubevirt.io/kubevirt/tests/framework/matcher"
	"kubevirt.io/kubevirt/tests/libmigration"
	"kubevirt.io/kubevirt/tests/libnet"
	"kubevirt.io/kubevirt/tests/libnet/cloudinit"
	"kubevirt.io/kubevirt/tests/libvmifact"
	"kubevirt.io/kubevirt/tests/libvmops"
	"kubevirt.io/kubevirt/tests/libwait"
	"kubevirt.io/kubevirt/tests/testsuite"
)

var _ = Describe("[sig-storage]VM state", decorators.SigStorage, decorators.SigStorageTPM, func() {
	var virtClient kubecli.KubevirtClient
	var err error

	BeforeEach(func() {
		virtClient, err = kubecli.GetKubevirtClient()
		Expect(err).ToNot(HaveOccurred())
	})

	Context("with persistent TPM VM option enabled", func() {
		startVM := func(vm *v1.VirtualMachine) {
			By("Starting the VM")
			vm = libvmops.StartVirtualMachine(vm)
			vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(context.Background(), vm.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Ensuring the firmware is done so we don't send any keystroke to it")
			// Wait for cloud init to finish and start the agent inside the vmi.
			Eventually(matcher.ThisVMI(vmi)).WithTimeout(4 * time.Minute).WithPolling(2 * time.Second).Should(matcher.HaveConditionTrue(v1.VirtualMachineInstanceAgentConnected))

			By("Logging in as root")
			err = console.LoginToFedora(vmi)
			Expect(err).ToNot(HaveOccurred())
		}

		migrateVMI := func(vmi *v1.VirtualMachineInstance) {
			By("Migrating the VMI")
			migration := libmigration.New(vmi.Name, vmi.Namespace)
			libmigration.RunMigrationAndExpectToCompleteWithDefaultTimeout(virtClient, migration)

		}

		addDataToTPM := func(vmi *v1.VirtualMachineInstance) {
			By("Storing a secret into the TPM")
			// https://www.intel.com/content/www/us/en/developer/articles/code-sample/protecting-secret-data-and-keys-using-intel-platform-trust-technology.html
			// Not sealing against a set of PCRs, out of scope here, but should work with a carefully selected set (at least PCR1 was seen changing accross reboots)
			Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
				&expect.BSnd{S: "tpm2_createprimary -Q --hierarchy=o --key-context=prim.ctx\n"},
				&expect.BExp{R: console.PromptExpression},
				&expect.BSnd{S: "echo MYSECRET | tpm2_create --hash-algorithm=sha256 --public=seal.pub --private=seal.priv --sealing-input=- --parent-context=prim.ctx\n"},
				&expect.BExp{R: console.PromptExpression},
				&expect.BSnd{S: "tpm2_load -Q --parent-context=prim.ctx --public=seal.pub --private=seal.priv --name=seal.name --key-context=seal.ctx\n"},
				&expect.BExp{R: console.PromptExpression},
				&expect.BSnd{S: "tpm2_evictcontrol --hierarchy=o --object-context=seal.ctx 0x81010002\n"},
				&expect.BExp{R: console.PromptExpression},
				&expect.BSnd{S: "tpm2_unseal -Q --object-context=0x81010002\n"},
				&expect.BExp{R: "MYSECRET"},
			}, 300)).To(Succeed(), "failed to store secret into the TPM")
		}

		checkTPM := func(vmi *v1.VirtualMachineInstance) {
			By("Ensuring the TPM is still functional and its state carried over")
			ExpectWithOffset(1, console.SafeExpectBatch(vmi, []expect.Batcher{
				&expect.BSnd{S: "tpm2_unseal -Q --object-context=0x81010002\n"},
				&expect.BExp{R: "MYSECRET"},
			}, 300)).To(Succeed(), "the state of the TPM did not persist")
		}

		addDataToEFI := func(vmi *v1.VirtualMachineInstance) {
			By("Creating an efivar")
			cmd := `printf "\x07\x00\x00\x00\x42" > /sys/firmware/efi/efivars/kvtest-12345678-1234-1234-1234-123456789abc`
			err = console.RunCommand(vmi, cmd, 10*time.Second)
			Expect(err).NotTo(HaveOccurred())
		}

		checkEFI := func(vmi *v1.VirtualMachineInstance) {
			By("Ensuring the efivar is present")
			Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
				&expect.BSnd{S: "hexdump /sys/firmware/efi/efivars/kvtest-12345678-1234-1234-1234-123456789abc\n"},
				&expect.BExp{R: "0042"},
			}, 10)).To(Succeed(), "expected efivar is missing")
		}

		DescribeTable("should persist VM state of", decorators.RequiresTwoSchedulableNodes, func(withTPM, withEFI bool, ops ...string) {
			By("Creating a migratable Fedora VM with UEFI")
			vmi := libvmifact.NewFedora(
				libvmi.WithInterface(libvmi.InterfaceDeviceWithMasqueradeBinding()),
				libvmi.WithNetwork(v1.DefaultPodNetwork()),
				libvmi.WithCloudInitNoCloud(libvmici.WithNoCloudNetworkData(cloudinit.CreateDefaultCloudInitNetworkData())),
				libvmi.WithUefi(false),
			)
			if withTPM {
				By("with persistent TPM enabled")
				vmi.Spec.Domain.Devices.TPM = &v1.TPMDevice{
					Persistent: pointer.BoolPtr(true),
				}
			}
			if withEFI {
				By("with persistent EFI enabled")
				vmi.Spec.Domain.Firmware = &v1.Firmware{
					Bootloader: &v1.Bootloader{
						EFI: &v1.EFI{SecureBoot: pointer.BoolPtr(false), Persistent: pointer.BoolPtr(true)},
					},
				}
			}
			vm := libvmi.NewVirtualMachine(vmi)
			vm, err = virtClient.VirtualMachine(testsuite.GetTestNamespace(vmi)).Create(context.Background(), vm, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			vmi.Namespace = vm.Namespace

			startVM(vm)

			if withTPM {
				addDataToTPM(vmi)
			}
			if withEFI {
				addDataToEFI(vmi)
			}

			for _, op := range ops {
				switch op {
				case "migrate":
					migrateVMI(vmi)
				case "restart":
					vm = libvmops.StopVirtualMachine(vm)
					startVM(vm)
				}
				if withTPM {
					checkTPM(vmi)
				}
				if withEFI {
					checkEFI(vmi)
				}
			}

			By("Stopping and removing the VM")
			err = virtClient.VirtualMachine(vm.Namespace).Stop(context.Background(), vm.Name, &v1.StopOptions{})
			Expect(err).ToNot(HaveOccurred())
			err = virtClient.VirtualMachine(testsuite.GetTestNamespace(vm)).Delete(context.Background(), vm.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		},
			Entry("[test_id:10818]TPM across migration and restart", true, false, "migrate", "restart"),
			Entry("[test_id:10819]TPM across restart and migration", true, false, "restart", "migrate"),
			Entry("[test_id:10820]EFI across migration and restart", false, true, "migrate", "restart"),
			Entry("[test_id:10821]TPM+EFI across migration and restart", true, true, "migrate", "restart"),
		)
		It("should remove persistent storage PVC if VMI is not owned by a VM", func() {
			By("Creating a VMI with persistent TPM enabled")
			vmi := libvmifact.NewFedora(libnet.WithMasqueradeNetworking())
			vmi.Spec.Domain.Devices.TPM = &v1.TPMDevice{
				Persistent: pointer.BoolPtr(true),
			}
			vmi, err = virtClient.VirtualMachineInstance(testsuite.GetTestNamespace(vmi)).Create(context.Background(), vmi, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the VMI to start")
			Eventually(func() error {
				vmi, err = virtClient.VirtualMachineInstance(vmi.Namespace).Get(context.Background(), vmi.Name, metav1.GetOptions{})
				return err
			}, 300*time.Second, 1*time.Second).Should(Succeed())
			libwait.WaitForSuccessfulVMIStart(vmi)

			By("Removing the VMI")
			err = virtClient.VirtualMachineInstance(testsuite.GetTestNamespace(vmi)).Delete(context.Background(), vmi.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Ensuring the PVC gets deleted")
			Eventually(func() error {
				_, err = virtClient.VirtualMachineInstance(vmi.Namespace).Get(context.Background(), vmi.Name, metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					return fmt.Errorf("VM %s not removed: %v", vmi.Name, err)
				}
				pvcs, err := virtClient.CoreV1().PersistentVolumeClaims(vmi.Namespace).List(context.Background(), metav1.ListOptions{
					LabelSelector: "persistent-state-for=" + vmi.Name,
				})
				Expect(err).ToNot(HaveOccurred())
				if len(pvcs.Items) > 0 {
					return fmt.Errorf("PVC %s not removed: %v", pvcs.Items[0].Name, err)
				}
				return nil
			}, 300*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
		})
	})
})
