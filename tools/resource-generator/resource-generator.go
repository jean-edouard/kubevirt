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
 * Copyright 2018 Red Hat, Inc.
 *
 */

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/virt-operator/resource/generate/components"
	"kubevirt.io/kubevirt/pkg/virt-operator/resource/generate/rbac"
	"kubevirt.io/kubevirt/tools/util"
)

const (
	featureGatesPlaceholder  = "FeatureGatesPlaceholder"
	infraReplicasPlaceholder = 255
)

func generateKubeVirtCR(namespace *string, imagePullPolicy v1.PullPolicy, featureGatesFlag *string, infraReplicasFlag *string) {
	var featureGates string
	if strings.HasPrefix(*featureGatesFlag, "{{") {
		featureGates = featureGatesPlaceholder
	} else {
		featureGates = *featureGatesFlag
	}
	var infraReplicas uint8
	if strings.HasPrefix(*infraReplicasFlag, "{{") {
		infraReplicas = infraReplicasPlaceholder
	} else {
		val, err := strconv.ParseUint(*infraReplicasFlag, 10, 8)
		if err != nil {
			panic(err)
		}
		infraReplicas = uint8(val)
	}
	var buf bytes.Buffer
	util.MarshallObject(components.NewKubeVirtCR(*namespace, imagePullPolicy, featureGates, infraReplicas), &buf)
	cr := buf.String()
	// The current function is sometimes used on yaml templates, and manipulates them as json/yaml above.
	// However, this only works for simple templates dealing with simple strings.
	// For list values, we need template code to iterate over the slice, and that code is not valid yaml.
	// To work around that, the variable name for the featureGates slice was treated as the first and only list item until now.
	// Therefore, if we're currently handling a template, the featureGates section looks like:
	//      featureGates:
	//      - {{.FeatureGates}}
	// however we want to treat the variable (".FeatureGates" here) as a slice and iterate over it (with a special case for empty list):
	//      featureGates:{{if .FeatureGates}}
	//      {{- range .FeatureGates}}
	//      - {{.}}
	//      {{- end}}{{else}} []{{end}}
	// The replace call below will transform the former into the latter, keeping the variable name ($2) and intendation ($1)
	if strings.HasPrefix(*featureGatesFlag, "{{") {
		featureGatesVar := strings.TrimPrefix(*featureGatesFlag, "{{")
		featureGatesVar = strings.TrimSuffix(featureGatesVar, "}}")
		re := regexp.MustCompile(`(?m)featureGates:\n([ \t]+)- ` + featureGatesPlaceholder)
		cr = re.ReplaceAllString(cr, `featureGates:{{if `+featureGatesVar+`}}
$1{{- range `+featureGatesVar+`}}
$1- {{.}}
$1{{- end}}{{else}} []{{end}}`)
	}

	if strings.HasPrefix(*infraReplicasFlag, "{{") {
		cr = strings.Replace(cr, fmt.Sprintf("replicas: %d", infraReplicasPlaceholder), "replicas: "+*infraReplicasFlag, 1)
	}

	fmt.Print(cr)
}

func main() {
	resourceType := flag.String("type", "", "Type of resource to generate. kv | kv-cr | operator-rbac | priorityclass")
	namespace := flag.String("namespace", "kube-system", "Namespace to use.")
	pullPolicy := flag.String("pullPolicy", "IfNotPresent", "ImagePullPolicy to use.")
	featureGates := flag.String("featureGates", "", "Feature gates to enable.")
	infraReplicas := flag.String("infraReplicas", "2", "Number of replicas for virt-controller and virt-api")

	flag.Parse()

	imagePullPolicy := v1.PullPolicy(*pullPolicy)

	switch *resourceType {
	case "kv":
		kv, err := components.NewKubeVirtCrd()
		if err != nil {
			panic(fmt.Errorf("This should not happen, %v", err))
		}
		util.MarshallObject(kv, os.Stdout)
	case "kv-cr":
		generateKubeVirtCR(namespace, imagePullPolicy, featureGates, infraReplicas)
	case "operator-rbac":
		all := rbac.GetAllOperator(*namespace)
		for _, r := range all {
			util.MarshallObject(r, os.Stdout)
		}
	case "priorityclass":
		priorityClass := components.NewKubeVirtPriorityClassCR()
		util.MarshallObject(priorityClass, os.Stdout)
	default:
		panic(fmt.Errorf("unknown resource type %s", *resourceType))
	}
}
