package heartbeat

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

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	kubevirtv1 "kubevirt.io/api/core/v1"

	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
	"kubevirt.io/kubevirt/pkg/testutils"
)

var _ = Describe("KSM", func() {
	var kubeClient *fake.Clientset
	var customKSMFilePath = "fake_path"

	createCustomKSMFile := func(value string) {
		customKSMFile, err := os.CreateTemp("", "mock_ksm_run")
		Expect(err).ToNot(HaveOccurred())
		defer customKSMFile.Close()
		_, err = customKSMFile.WriteString(value)
		Expect(err).ToNot(HaveOccurred())
		customKSMFilePath = customKSMFile.Name()
	}

	BeforeEach(func() {
		createCustomKSMFile("1")
		kubeClient = fake.NewSimpleClientset()
	})
	AfterEach(func() {
		diskutils.RemoveFilesIfExist(customKSMFilePath)
	})

	It("should add KSM label", func() {
		testutils.ExpectNodePatch(kubeClient, kubevirtv1.KSMEnabledLabel)
		// Execute heartbeat
		// Expect(res).To(BeTrue())
	})

	Describe(", when ksmConfiguration is provided,", func() {
		var kv *kubevirtv1.KubeVirt
		BeforeEach(func() {
			kv = &kubevirtv1.KubeVirt{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubevirt",
					Namespace: "kubevirt",
				},
				Spec: kubevirtv1.KubeVirtSpec{
					Configuration: kubevirtv1.KubeVirtConfiguration{
						KSMConfiguration: &kubevirtv1.KSMConfiguration{
							NodeLabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"test_label": "true",
								},
							},
						},
					},
				},
			}
			_, _, _ = testutils.NewFakeClusterConfigUsingKV(kv)
		})

		DescribeTable("should", func(initialKsmValue string, nodeLabels, nodeAnnotations map[string]string, expectedNodePatch []string, expectedKsmValue string) {
			testutils.ExpectNodePatch(kubeClient, expectedNodePatch...)
			// Execute heartbeat
			// Expect(res).To(BeTrue())
			Expect(os.ReadFile(customKSMFilePath)).To(BeEquivalentTo([]byte(expectedKsmValue)))
		},
			Entry("enable ksm if the node labels match ksmConfiguration.nodeLabelSelector",
				"0\n", map[string]string{"test_label": "true"}, make(map[string]string),
				[]string{kubevirtv1.KSMEnabledLabel, kubevirtv1.KSMHandlerManagedAnnotation}, "1\n",
			),
			Entry("disable ksm if the node labels does not match ksmConfiguration.nodeLabelSelector and the node has the KSMHandlerManagedAnnotation annotation",
				"1\n", map[string]string{"test_label": "false"}, map[string]string{kubevirtv1.KSMHandlerManagedAnnotation: "true"},
				[]string{kubevirtv1.KSMHandlerManagedAnnotation}, "0\n",
			),
			Entry("not change ksm if the node labels does not match ksmConfiguration.nodeLabelSelector and the node does not have the KSMHandlerManagedAnnotation annotation",
				"1\n", map[string]string{"test_label": "false"}, make(map[string]string),
				nil, "1\n",
			),
		)
	})

})
