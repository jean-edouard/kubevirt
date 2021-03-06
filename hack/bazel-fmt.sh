#!/bin/bash
#
# This file is part of the KubeVirt project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Copyright 2019 Red Hat, Inc.
#

set -e

source hack/common.sh
source hack/config.sh

shfmt -i 4 -w ${KUBEVIRT_DIR}/hack/ ${KUBEVIRT_DIR}/images/
bazel run \
    --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64_cgo \
    --workspace_status_command=./hack/print-workspace-status.sh \
    --host_force_python=${bazel_py} \
    //:gazelle -- pkg/ tools/ tests/ cmd/
bazel run \
    --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64_cgo \
    --workspace_status_command=./hack/print-workspace-status.sh \
    --host_force_python=${bazel_py} \
    //:goimports
# allign BAZEL files to a single format
bazel run \
    --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64_cgo \
    --workspace_status_command=./hack/print-workspace-status.sh \
    --host_force_python=${bazel_py} \
    //:buildifier
