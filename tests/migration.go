package tests

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"time"

	"kubevirt.io/kubevirt/tests/flags"
	"kubevirt.io/kubevirt/tests/util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	k8snetworkplumbingwgv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "kubevirt.io/client-go/apis/core/v1"
	"kubevirt.io/client-go/kubecli"
)

func ExpectMigrationSuccess(virtClient kubecli.KubevirtClient, migration *v1.VirtualMachineInstanceMigration, timeout int) string {
	By("Waiting until the Migration Completes")
	uid := ""
	Eventually(func() error {
		migration, err := virtClient.VirtualMachineInstanceMigration(migration.Namespace).Get(migration.Name, &metav1.GetOptions{})
		if err != nil {
			return err
		}

		Expect(migration.Status.Phase).ToNot(Equal(v1.MigrationFailed), "migration should not fail")

		uid = string(migration.UID)
		if migration.Status.Phase == v1.MigrationSucceeded {
			return nil
		}
		return fmt.Errorf("migration is in the phase: %s", migration.Status.Phase)

	}, timeout, 1*time.Second).ShouldNot(HaveOccurred(), fmt.Sprintf("migration should succeed after %d s", timeout))
	return uid
}

func RunMigrationAndExpectCompletion(virtClient kubecli.KubevirtClient, migration *v1.VirtualMachineInstanceMigration, timeout int) string {
	By("Starting a Migration")
	var err error
	var migrationCreated *v1.VirtualMachineInstanceMigration
	Eventually(func() error {
		migrationCreated, err = virtClient.VirtualMachineInstanceMigration(migration.Namespace).Create(migration)
		return err
	}, timeout, 1*time.Second).Should(Succeed(), "migration creation should succeed")
	migration = migrationCreated

	return ExpectMigrationSuccess(virtClient, migration, timeout)
}

func ConfirmVMIPostMigration(virtClient kubecli.KubevirtClient, vmi *v1.VirtualMachineInstance, migrationUID string) *v1.VirtualMachineInstance {
	By("Retrieving the VMI post migration")
	vmi, err := virtClient.VirtualMachineInstance(vmi.Namespace).Get(vmi.Name, &metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred(), "should have been able to retrive the VMI instance")

	By("Verifying the VMI's migration state")
	Expect(vmi.Status.MigrationState).ToNot(BeNil(), "should have been able to retrieve the VMIs `Status::MigrationState`")
	Expect(vmi.Status.MigrationState.StartTimestamp).ToNot(BeNil(), "the VMIs `Status::MigrationState` should have a StartTimestamp")
	Expect(vmi.Status.MigrationState.EndTimestamp).ToNot(BeNil(), "the VMIs `Status::MigrationState` should have a EndTimestamp")
	Expect(vmi.Status.MigrationState.TargetNode).To(Equal(vmi.Status.NodeName), "the VMI should have migrated to the desired node")
	Expect(vmi.Status.MigrationState.TargetNode).NotTo(Equal(vmi.Status.MigrationState.SourceNode), "the VMI must have migrated to a different node from the one it originated from")
	Expect(vmi.Status.MigrationState.Completed).To(BeTrue(), "the VMI migration state must have completed")
	Expect(vmi.Status.MigrationState.Failed).To(BeFalse(), "the VMI migration status must not have failed")
	Expect(vmi.Status.MigrationState.TargetNodeAddress).NotTo(Equal(""), "the VMI `Status::MigrationState::TargetNodeAddress` must not be empty")
	Expect(string(vmi.Status.MigrationState.MigrationUID)).To(Equal(migrationUID), "the VMI migration UID must be the expected one")

	By("Verifying the VMI's is in the running state")
	Expect(vmi.Status.Phase).To(Equal(v1.Running), "the VMI must be in `Running` state after the migration")

	return vmi
}

func SetDedicatedMigrationNetwork(nad string) *v1.KubeVirt {
	virtClient, err := kubecli.GetKubevirtClient()
	Expect(err).ToNot(HaveOccurred())

	kv := util.GetCurrentKv(virtClient)

	if kv.Spec.Configuration.MigrationConfiguration == nil {
		kv.Spec.Configuration.MigrationConfiguration = &v1.MigrationConfiguration{
			DedicatedMigrationNetwork: &nad,
		}
	}

	kv.Spec.Configuration.MigrationConfiguration.DedicatedMigrationNetwork = &nad

	return UpdateKubeVirtConfigValueAndWait(kv.Spec.Configuration)
}

func ClearDedicatedMigrationNetwork() *v1.KubeVirt {
	virtClient, err := kubecli.GetKubevirtClient()
	Expect(err).ToNot(HaveOccurred())

	kv := util.GetCurrentKv(virtClient)

	if kv.Spec.Configuration.MigrationConfiguration != nil {
		kv.Spec.Configuration.MigrationConfiguration.DedicatedMigrationNetwork = nil
	}

	return UpdateKubeVirtConfigValueAndWait(kv.Spec.Configuration)
}

func GenerateMigrationCNINetworkAttachmentDefinition() *k8snetworkplumbingwgv1.NetworkAttachmentDefinition {
	nad := &k8snetworkplumbingwgv1.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "migration-cni",
			Namespace: flags.KubeVirtInstallNamespace,
		},
		Spec: k8snetworkplumbingwgv1.NetworkAttachmentDefinitionSpec{
			Config: `{
      "cniVersion": "0.3.1",
      "name": "migration-bridge",
      "type": "macvlan",
      "master": "eth1",
      "mode": "bridge",
      "ipam": {
        "type": "whereabouts",
        "range": "10.1.1.0/24"
      }
}`,
		},
	}

	return nad
}

func EnsureNoMigrationMetadataInPersistentXML(vmi *v1.VirtualMachineInstance) {
	domXML := RunCommandOnVmiPod(vmi, []string{"virsh", "dumpxml", "1"})
	decoder := xml.NewDecoder(bytes.NewReader([]byte(domXML)))

	var location = make([]string, 0)
	var found = false
	for {
		token, err := decoder.RawToken()
		if err == io.EOF {
			break
		}
		Expect(err).To(BeNil(), "error getting token: %v\n", err)

		switch v := token.(type) {
		case xml.StartElement:
			location = append(location, v.Name.Local)

			if len(location) >= 4 &&
				location[0] == "domain" &&
				location[1] == "metadata" &&
				location[2] == "kubevirt" &&
				location[3] == "migration" {
				found = true
			}
			Expect(found).To(BeFalse(), "Unexpected KubeVirt migration metadata found in domain XML")
		case xml.EndElement:
			location = location[:len(location)-1]
		}

	}
}
