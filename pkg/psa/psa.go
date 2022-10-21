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
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2022 Red Hat, Inc.
 *
 */

package psa

import (
	"context"
	"fmt"

	k8sv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/client-go/kubecli"
)

const PSALabel = "pod-security.kubernetes.io/enforce"
const OpenshiftPSAsync = "security.openshift.io/scc.podSecurityLabelSync"

func EscalateNamespace(namespaceStore cache.Store, client kubecli.KubevirtClient, namespace string, onOpenshift bool) error {
	isPrivileged, err := IsNamespacePrivilegedWithStore(namespaceStore, client, namespace)
	if err != nil {
		return err
	}

	if !isPrivileged {
		labels := ""
		if !onOpenshift {
			labels = fmt.Sprintf(`{"%s": "privileged"}`, PSALabel)
		} else {
			labels = fmt.Sprintf(`{"%s": "privileged", "%s": "false"}`, PSALabel, OpenshiftPSAsync)
		}
		data := []byte(fmt.Sprintf(`{"metadata": { "labels": %s}}`, labels))
		_, err := client.CoreV1().Namespaces().Patch(context.TODO(), namespace, types.StrategicMergePatchType, data, v1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("Failed to apply enforce label on namespace %s: %w", namespace, err)
		}
	}
	return nil
}

func IsNamespacePrivilegedWithStore(namespaceStore cache.Store, client kubecli.KubevirtClient, namespace string) (bool, error) {
	obj, exists, err := namespaceStore.GetByKey(namespace)
	if err != nil {
		return false, fmt.Errorf("Failed to get namespace, %w", err)
	}
	if !exists {
		return false, fmt.Errorf("Namespace %s not observed, %w", namespace, err)
	}
	return IsNamespacePrivileged(obj.(*k8sv1.Namespace)), nil
}

func IsNamespacePrivileged(namespace *k8sv1.Namespace) bool {
	enforceLevel, labelExist := namespace.Labels[PSALabel]
	return labelExist && enforceLevel == "privileged"
}